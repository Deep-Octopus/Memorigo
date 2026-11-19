#!/bin/sh
# Download the LongMemEval oracle file and verify its SHA-256.
# The JSON is gitignored. This does not score the benchmark.
set -eu
cd "$(dirname "$0")"

name=longmemeval_oracle.json
url=https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_oracle.json
sum=$(awk -v name="$name" '$2 == name { print $1; exit }' checksums.txt)
if [ -z "$sum" ]; then
	echo "missing checksum for $name" >&2
	exit 1
fi

checksum() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
		return
	fi
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
		return
	fi
	echo "need shasum or sha256sum" >&2
	exit 1
}

tmp="$name.partial"
curl -fsSL --retry 3 --retry-delay 2 -o "$tmp" "$url"
got=$(checksum "$tmp")
if [ "$got" != "$sum" ]; then
	echo "checksum mismatch for $name: got $got want $sum" >&2
	rm -f "$tmp"
	exit 1
fi
mv "$tmp" "$name"
echo "verified $name ($sum)"
