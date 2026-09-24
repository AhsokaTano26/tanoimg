package app

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
)

func isSVGHead(head []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(head))
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		switch value := token.(type) {
		case xml.StartElement:
			return value.Name.Local == "svg" && (value.Name.Space == "" || value.Name.Space == "http://www.w3.org/2000/svg")
		case xml.Directive:
			return false
		case xml.CharData:
			if len(bytes.TrimSpace(value)) != 0 {
				return false
			}
		}
	}
}

func inspectImageFormat(f *os.File, format string) (string, error) {
	if format == "svg" {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		decoder := xml.NewDecoder(f)
		root := false
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("SVG 内容无效: %w", err)
			}
			switch value := token.(type) {
			case xml.Directive:
				return "", errors.New("SVG 不允许 XML 指令")
			case xml.ProcInst:
				if value.Target != "xml" {
					return "", errors.New("SVG 不允许处理指令")
				}
			case xml.StartElement:
				if !root {
					if value.Name.Local != "svg" || (value.Name.Space != "" && value.Name.Space != "http://www.w3.org/2000/svg") {
						return "", errors.New("SVG 根元素无效")
					}
					root = true
				}
			}
		}
		if !root {
			return "", errors.New("SVG 内容无效")
		}
	}
	if format == "png" {
		animated, err := isAPNG(f)
		if err != nil {
			return "", err
		}
		if animated {
			return "apng", nil
		}
	}
	return format, nil
}

func isAPNG(f *os.File) (bool, error) {
	if _, err := f.Seek(8, io.SeekStart); err != nil {
		return false, err
	}
	var chunk [8]byte
	for i := 0; i < 1000; i++ {
		if _, err := io.ReadFull(f, chunk[:]); err != nil {
			return false, nil
		}
		size := binary.BigEndian.Uint32(chunk[:4])
		switch string(chunk[4:]) {
		case "acTL":
			return true, nil
		case "IDAT", "IEND":
			return false, nil
		}
		if _, err := f.Seek(int64(size)+4, io.SeekCurrent); err != nil {
			return false, err
		}
	}
	return false, nil
}
