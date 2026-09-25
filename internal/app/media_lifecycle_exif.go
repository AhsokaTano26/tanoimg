package app

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// JPEG APP1 (EXIF/XMP), APP13 (IPTC), and COM can carry location and other
// personal metadata. Copying the compressed scan preserves image pixels.
func stripJPEGMetadata(input io.ReadSeeker, output io.Writer) (retErr error) {
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(input, 64<<10)
	writer := bufio.NewWriterSize(output, 64<<10)
	defer func() {
		if err := writer.Flush(); retErr == nil {
			retErr = err
		}
	}()
	var soi [2]byte
	if _, err := io.ReadFull(reader, soi[:]); err != nil {
		return err
	}
	if soi != [2]byte{0xff, 0xd8} {
		return errors.New("无效 JPEG 内容")
	}
	if _, err := writer.Write(soi[:]); err != nil {
		return err
	}
	pending := byte(0)
	for {
		var marker [2]byte
		if pending != 0 {
			marker = [2]byte{0xff, pending}
			pending = 0
		} else if _, err := io.ReadFull(reader, marker[:]); err != nil {
			return err
		}
		if marker[0] != 0xff || marker[1] == 0x00 {
			return errors.New("无效 JPEG 标记")
		}
		for marker[1] == 0xff {
			if _, err := io.ReadFull(reader, marker[1:]); err != nil {
				return err
			}
		}
		if marker[1] == 0xd9 || marker[1] == 0x01 || marker[1] >= 0xd0 && marker[1] <= 0xd7 {
			if _, err := writer.Write(marker[:]); err != nil {
				return err
			}
			if marker[1] == 0xd9 {
				return nil
			}
			continue
		}
		var length [2]byte
		if _, err := io.ReadFull(reader, length[:]); err != nil {
			return err
		}
		size := int(binary.BigEndian.Uint16(length[:]))
		if size < 2 {
			return errors.New("无效 JPEG 段长度")
		}
		payload := make([]byte, size-2)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return err
		}
		if marker[1] == 0xe1 {
			if orientation := exifOrientation(payload); orientation > 1 {
				return errors.New("JPEG 包含旋转方向信息，请先将图片旋正后再移除 EXIF")
			}
		}
		if marker[1] != 0xe1 && marker[1] != 0xed && marker[1] != 0xfe {
			if _, err := writer.Write(marker[:]); err != nil {
				return err
			}
			if _, err := writer.Write(length[:]); err != nil {
				return err
			}
			if _, err := writer.Write(payload); err != nil {
				return err
			}
		}
		if marker[1] == 0xda {
			var err error
			pending, err = copyJPEGEntropy(reader, writer)
			if err != nil {
				return err
			}
		}
	}
}

func copyJPEGEntropy(reader *bufio.Reader, writer *bufio.Writer) (byte, error) {
	for {
		value, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		if value != 0xff {
			if err := writer.WriteByte(value); err != nil {
				return 0, err
			}
			continue
		}
		marker, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		for marker == 0xff {
			marker, err = reader.ReadByte()
			if err != nil {
				return 0, err
			}
		}
		if marker == 0x00 || marker >= 0xd0 && marker <= 0xd7 {
			if err := writer.WriteByte(0xff); err != nil {
				return 0, err
			}
			if err := writer.WriteByte(marker); err != nil {
				return 0, err
			}
			continue
		}
		return marker, nil
	}
}

func exifOrientation(payload []byte) uint16 {
	if len(payload) < 14 || string(payload[:6]) != "Exif\x00\x00" {
		return 0
	}
	tiff := payload[6:]
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 0
	}
	offset := int(order.Uint32(tiff[4:8]))
	if offset < 0 || offset+2 > len(tiff) {
		return 0
	}
	count := int(order.Uint16(tiff[offset : offset+2]))
	for i := 0; i < count; i++ {
		entry := offset + 2 + 12*i
		if entry+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[entry:entry+2]) == 0x0112 && order.Uint16(tiff[entry+2:entry+4]) == 3 && order.Uint32(tiff[entry+4:entry+8]) == 1 {
			return order.Uint16(tiff[entry+8 : entry+10])
		}
	}
	return 0
}

func (a *App) maybeStripJPEGMetadata(source *os.File, format string, enabled bool) (*os.File, int64, error) {
	if !enabled || (format != "jpg" && format != "jpeg") {
		st, err := source.Stat()
		if err != nil {
			return nil, 0, err
		}
		return source, st.Size(), nil
	}
	out, err := os.CreateTemp(filepath.Join(a.DataDir, "uploads"), ".strip-jpeg-*")
	if err != nil {
		return nil, 0, err
	}
	if err := stripJPEGMetadata(source, out); err != nil {
		out.Close()
		os.Remove(out.Name())
		return nil, 0, fmt.Errorf("移除 JPEG 元数据失败: %w", err)
	}
	st, err := out.Stat()
	if err != nil {
		out.Close()
		os.Remove(out.Name())
		return nil, 0, err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(out.Name())
		return nil, 0, err
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		out.Close()
		os.Remove(out.Name())
		return nil, 0, err
	}
	return out, st.Size(), nil
}
