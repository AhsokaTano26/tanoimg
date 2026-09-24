#!/usr/bin/env python3
"""Plot archived measurements; never launches a benchmark or modifies raw data."""
import argparse
import hashlib
import json
from pathlib import Path

import matplotlib
matplotlib.use('Agg')
from matplotlib import font_manager, ft2font
import matplotlib.pyplot as plt
from matplotlib.text import Text
from matplotlib.ticker import ScalarFormatter
import numpy as np

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / 'docs/benchmarks'
PROFILES = ['native', '1g', '2g']
LABELS = ['本机原生', '1 GiB / 2 vCPU', '2 GiB / 2 vCPU']
COLORS = ['#456D90', '#B97A48', '#628575']
MARKERS = ['o', 's', '^']
STYLES = ['-', '--', '-.']


def read(name):
    rows = [json.loads(line) for line in (DATA / f'2026-09-25-{name}.jsonl').read_text().splitlines() if line.strip()]
    for row in rows:
        assert abs(row['success_rps'] - row['success'] / row['seconds']) < 1e-6
        assert abs(row['error_pct'] - 100 * (row['requests'] - row['success']) / row['requests']) < 1e-6
    return rows


def panel(ax, label, ylabel):
    ax.set_ylabel(ylabel)
    ax.text(0, 1.04, label, transform=ax.transAxes, fontsize=10)
    ax.spines[['top', 'right']].set_visible(False)
    ax.grid(axis='y', color='#dddddd', linewidth=.45)
    ax.set_axisbelow(True)
    ax.set_ylim(bottom=0)


def curves(ax, rows, field, profiles=PROFILES):
    for profile in profiles:
        i = PROFILES.index(profile)
        subset = sorted((r for r in rows if r['profile'] == profile), key=lambda r: r['concurrency'])
        ax.plot([r['concurrency'] for r in subset], [r[field] for r in subset],
                color=COLORS[i], marker=MARKERS[i], linestyle=STYLES[i], label=LABELS[i])
    ticks = sorted({r['concurrency'] for r in rows})
    ax.set_xscale('log', base=2)
    ax.set_xticks(ticks)
    ax.xaxis.set_major_formatter(ScalarFormatter())
    ax.set_xlabel('客户端并发数（对数刻度）')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--simsun', type=Path, help='Path to a licensed SimSun font, if not installed')
    parser.add_argument('--times', type=Path, help='Path to a licensed Times New Roman font, if not installed')
    parser.add_argument('--output', type=Path, default=DATA / 'figures')
    args = parser.parse_args()
    for path in [args.simsun, args.times]:
        if path:
            font_manager.fontManager.addfont(str(path))
    fonts = [font_manager.findfont(f, fallback_to_default=False) for f in ['Times New Roman', 'SimSun']]
    faces = [ft2font.FT2Font(f) for f in fonts]
    plt.rcParams.update({'font.family': ['Times New Roman', 'SimSun'], 'font.size': 9,
                         'axes.labelsize': 9, 'legend.fontsize': 8, 'legend.frameon': False,
                         'axes.linewidth': .7, 'lines.linewidth': 1.2, 'lines.markersize': 4,
                         'pdf.fonttype': 42, 'ps.fonttype': 42, 'figure.facecolor': 'white',
                         'savefig.facecolor': 'white', 'axes.formatter.use_mathtext': False})
    args.output.mkdir(parents=True, exist_ok=True)
    manifest = {'fonts': [f.family_name for f in faces], 'matplotlib': matplotlib.__version__,
                'dpi': 400, 'sources': {}, 'figures': []}
    for p in sorted(DATA.glob('2026-09-25-*.jsonl')):
        manifest['sources'][p.name] = hashlib.sha256(p.read_bytes()).hexdigest()

    def save(fig, name):
        fig.canvas.draw()
        # Validate every actual label/tick and enforce the intended glyph fallback.
        for artist in fig.findobj(Text):
            for char in artist.get_text():
                if char.isspace():
                    continue
                cp = ord(char)
                if '\u4e00' <= char <= '\u9fff':
                    assert not faces[0].get_char_index(cp), f'Times unexpectedly covers {char}'
                    assert faces[1].get_char_index(cp), f'SimSun missing {char}'
                else:
                    assert any(f.get_char_index(cp) for f in faces), f'Missing glyph {char}'
                if char.isascii() and char.isalnum():
                    assert faces[0].get_char_index(cp), f'Times missing {char}'
        for ext in ['png', 'pdf']:
            fig.savefig(args.output / f'{name}.{ext}', dpi=400, bbox_inches='tight', pad_inches=.08)
        manifest['figures'].append(name)
        plt.close(fig)

    sweep = read('sweep')
    assert len(sweep) == 36
    for name, fields, units in [
        ('upload_concurrency', ['success_rps', 'error_pct'], ['成功吞吐 / 张每秒', '错误率 / %']),
        ('upload_latency_memory', ['p95_ms', 'server_peak_rss_mib'], ['成功请求 P95 延迟 / ms', '服务进程峰值 RSS / MiB'])]:
        fig, axes = plt.subplots(2, 2, figsize=(7.1, 5.5), layout='constrained')
        for col, (side, size) in enumerate([(256, '256 KiB'), (724, '2 MiB')]):
            rows = [r for r in sweep if r['side'] == side]
            for row, (field, unit) in enumerate(zip(fields, units)):
                ax = axes[row, col]
                curves(ax, rows, field)
                panel(ax, f'({chr(97 + row * 2 + col)})  单图约 {size}', unit)
                if field == 'error_pct':
                    ax.set_ylim(-2, 105)
        axes[0, 0].legend(loc='best')
        save(fig, name)

    batch = read('batch')
    assert len(batch) == 6 and all(r['success'] == 10000 and r['error_pct'] == 0 for r in batch)
    fig, axes = plt.subplots(1, 2, figsize=(7.1, 3.2), layout='constrained')
    for ax, field, label, unit in zip(axes, ['seconds', 'server_peak_rss_mib'], ['(a)', '(b)'], ['万张实际耗时 / s', '服务进程峰值 RSS / MiB']):
        for j, side in enumerate([256, 724]):
            values = [next(r[field] for r in batch if r['profile'] == p and r['side'] == side) for p in PROFILES]
            bars = ax.bar(np.arange(3) + (j - .5) * .36, values, .34,
                          color=['#456D90', '#B97A48'][j], hatch=['', '///'][j],
                          label=['约 256 KiB', '约 2 MiB'][j], edgecolor='white', linewidth=.5)
            ax.bar_label(bars, fmt='%.1f', padding=3, fontsize=8)
        ax.set_xticks(range(3), ['本机原生', '1 GiB', '2 GiB'])
        panel(ax, label, unit)
        ax.set_ylim(0, max(r[field] for r in batch) * 1.32)
    axes[1].legend(loc='upper right', ncol=2)
    save(fig, 'batch_upload')

    conversion = read('convert-jpg') + read('webp-sustained')
    fig, axes = plt.subplots(1, 2, figsize=(7.1, 3.2), layout='constrained')
    for ax, field, label, unit in zip(axes, ['success_rps', 'server_peak_rss_mib'], ['(a)', '(b)'], ['成功吞吐 / 张每秒', '服务进程峰值 RSS / MiB']):
        for j, p in enumerate(PROFILES[1:]):
            values = [next(r[field] for r in conversion if r['profile'] == p and r[key]) for key in ['convert_jpg', 'convert_webp']]
            bars = ax.bar(np.arange(2) + (j - .5) * .36, values, .34, color=COLORS[j + 1],
                          hatch=['///', '...'][j], label=LABELS[j + 1], edgecolor='white', linewidth=.5)
            ax.bar_label(bars, fmt='%.2f' if field == 'success_rps' else '%.1f', padding=3, fontsize=8)
        ax.set_xticks(range(2), ['PNG → JPEG', 'PNG → WebP'])
        panel(ax, label, unit)
        ax.set_ylim(0, max(r[field] for r in conversion) * 1.32)
    axes[1].legend(loc='upper left')
    save(fig, 'conversion_pressure')

    fig, axes = plt.subplots(2, 2, figsize=(7.1, 5.5), layout='constrained')
    for row, (name, label) in enumerate([('gallery', '图库分页'), ('download', '本机原图热读')]):
        rows = read(name)
        for col, (field, unit) in enumerate([('success_rps', '成功吞吐 / 请求每秒'), ('p95_ms', '成功请求 P95 延迟 / ms')]):
            ax = axes[row, col]
            curves(ax, rows, field, PROFILES if row == 0 else ['native'])
            panel(ax, f'({chr(97 + row * 2 + col)})  {label}', unit)
    axes[0, 0].legend(loc='best')
    save(fig, 'read_concurrency')
    (args.output / 'manifest.json').write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + '\n')
    print(json.dumps(manifest, indent=2, ensure_ascii=False))


if __name__ == '__main__':
    main()
