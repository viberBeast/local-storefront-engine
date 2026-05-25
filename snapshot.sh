#!/bin/bash

OUTPUT_FILE="codebase_snapshot.txt"

echo "=== CODEBASE SNAPSHOT GENERATED ON $(date) ===" > $OUTPUT_FILE
echo "" >> $OUTPUT_FILE
echo "=== PROJECT DIRECTORY STRUCTURE ===" >> $OUTPUT_FILE

# 1. Capture clean tree view (skipping git/heavy databases)
if command -v tree &> /dev/null; then
    tree -I '.git|*.db|*.db-wal|*.db-shm|store-engine*' >> $OUTPUT_FILE
else
    find . -maxdepth 3 ! -path '*/.*' ! -name '*.db*' ! -name 'store-engine*' >> $OUTPUT_FILE
fi

echo "" >> $OUTPUT_FILE
echo "=== SOURCE FILE CONTENTS ===" >> $OUTPUT_FILE

# 2. Bulletproof file loop targeting Go, HTML, and CSS source files
find cmd internal web -type f -name "*.go" -o -name "*.html" -o -name "*.css" 2>/dev/null | while read -r file; do
    echo "" >> $OUTPUT_FILE
    echo "--------------------------------------------------" >> $OUTPUT_FILE
    echo "FILE: $file" >> $OUTPUT_FILE
    echo "--------------------------------------------------" >> $OUTPUT_FILE
    cat "$file" >> $OUTPUT_FILE
done

echo "✓ Snapshot built successfully: $OUTPUT_FILE"
