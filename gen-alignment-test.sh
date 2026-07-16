#!/usr/bin/env bash
# Copyright 2026 marshone
# Licensed under the Apache License, Version 2.0

set -euo pipefail

if [ "$#" -ne 1 ]; then
    echo "Usage: $0 <target-package-directory>"
    echo "Example: $0 ./internal/control"
    exit 1
fi

TARGET_DIR="$1"

if [ ! -d "$TARGET_DIR" ]; then
    echo "Error: Directory '$TARGET_DIR' does not exist."
    exit 1
fi

# Extract the package name from Go files in the target directory
PKG_NAME=$(grep -r "package " "$TARGET_DIR" --include="*.go" --exclude="*_test.go" -h | head -n 1 | awk '{print $2}')

if [ -z "$PKG_NAME" ]; then
    echo "Error: Could not determine package name in '$TARGET_DIR'."
    exit 1
fi

# Find all structs (both public and unexported)
mapfile -t STRUCTS < <(grep -r "type " "$TARGET_DIR" --include="*.go" --exclude="*_test.go" -h \
    | grep -E "type[[:space:]]+[A-Za-z0-9_]+[[:space:]]+struct" \
    | sed -E 's/.*type[[:space:]]+([A-Za-z0-9_]+)[[:space:]]+struct.*/\1/' \
    | sort -u)

if [ ${#STRUCTS[@]} -eq 0 ]; then
    echo "No structs found in '$TARGET_DIR'."
    exit 0
fi

echo "Generating alignment test for package '$PKG_NAME' with ${#STRUCTS[@]} structs..."

OUT_FILE="${TARGET_DIR}/struct_alignment_test.go"

{
    echo "package $PKG_NAME"
    echo ""
    echo "import ("
    echo "	\"testing\""
    echo ""
    echo "	\"github.com/marshone/aligncheck\""
    echo ")"
    echo ""
    echo "func TestStructAlignments(t *testing.T) {"
    echo "	registry := map[string]interface{}{"
    for struct in "${STRUCTS[@]}"; do
        echo "		\"$struct\": $struct{},"
    done
    echo "	}"
    echo ""
    echo "	aligncheck.AssertAllInPackageAligned(t, registry)"
    echo "}"
} > "$OUT_FILE"

# Run gofmt on the generated file if gofmt is available
if command -v gofmt >/dev/null 2>&1; then
    gofmt -w "$OUT_FILE"
fi

echo "Successfully wrote $OUT_FILE! Run 'go test -v ./$TARGET_DIR/...' to verify."
