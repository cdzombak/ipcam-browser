#!/usr/bin/env python3
"""
Strip HXVS/HXVF 16-byte headers from a raw H.264 stream and mux to MP4.

Usage:
  python h264_clean_and_mux.py INPUT.264 [-o OUTPUT.mp4] [--fps 20] [--keep-clean]
"""
import argparse
import subprocess
import sys
from pathlib import Path


def strip_headers(src: Path, cleaned: Path) -> None:
    data = src.read_bytes()
    out = bytearray()
    i = 0
    removed = 0
    headers = (b"HXVS", b"HXVF")
    length = len(data)

    while i < length:
        if i + 16 <= length and data[i : i + 4] in headers:
            i += 16
            removed += 16
            continue
        out.append(data[i])
        i += 1

    cleaned.write_bytes(out)
    print(f"Stripped {removed} bytes of HXVS/HXVF headers -> {cleaned}")


def detect_fps(path: Path) -> int | None:
    """Try to read fps from the stream with ffprobe."""
    cmd = [
        "ffprobe",
        "-v",
        "error",
        "-select_streams",
        "v:0",
        "-show_entries",
        "stream=r_frame_rate,avg_frame_rate",
        "-of",
        "default=nk=1:nw=1",
        str(path),
    ]
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True, check=True)
    except subprocess.CalledProcessError:
        return None

    for line in proc.stdout.splitlines():
        if "/" in line:
            num, den = line.split("/", 1)
            try:
                num = float(num)
                den = float(den)
                if den != 0:
                    val = num / den
                    if val > 0:
                        return round(val)
            except ValueError:
                continue
        else:
            try:
                val = float(line)
                if val > 0:
                    return round(val)
            except ValueError:
                continue
    return None


def mux_to_mp4(cleaned: Path, mp4_path: Path, fps: int) -> None:
    cmd = [
        "ffmpeg",
        "-y",
        "-fflags",
        "+genpts",
        "-framerate",
        str(fps),
        "-i",
        str(cleaned),
        "-c",
        "copy",
        "-movflags",
        "+faststart",
        str(mp4_path),
    ]
    subprocess.run(cmd, check=True)
    print(f"Muxed to {mp4_path}")


def main() -> None:
    parser = argparse.ArgumentParser(description="Clean HXVS/HXVF headers and mux to MP4")
    parser.add_argument("input", type=Path, help="Raw .264 file")
    parser.add_argument("-o", "--output", type=Path, help="Output MP4 (default: <input>_clean.mp4)")
    parser.add_argument("--fps", type=int, help="Frame rate override; if not set, try auto-detect, else 20")
    parser.add_argument("--keep-clean", action="store_true", help="Keep intermediate .clean.264 file")
    args = parser.parse_args()

    input_path = args.input
    if not input_path.exists():
        raise SystemExit(f"Input not found: {input_path}")

    cleaned_path = input_path.with_suffix(".clean.264")
    strip_headers(input_path, cleaned_path)

    mp4_out = args.output or input_path.with_name(f"{input_path.stem}_clean.mp4")

    fps = args.fps
    if fps is None:
        fps = detect_fps(cleaned_path)
        if fps:
            print(f"Detected fps: {fps}")
        else:
            fps = 20
            print("Could not detect fps; defaulting to 20. Use --fps to override.", file=sys.stderr)

    mux_to_mp4(cleaned_path, mp4_out, fps)

    if not args.keep_clean:
        cleaned_path.unlink(missing_ok=True)


if __name__ == "__main__":
    main()
