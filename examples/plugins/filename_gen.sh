#!/bin/bash
# Filename generator plugin for filehook
#
# Input: First argument is the input file path
# Output: JSON with "outputs" array of relative paths
#
# This example converts input.txt -> input.upper.txt

INPUT_PATH="$1"
BASENAME=$(basename "$INPUT_PATH")
NAME="${BASENAME%.*}"
EXT="${BASENAME##*.}"

# Generate output filename
OUTPUT_NAME="${NAME}.upper.${EXT}"

# Output JSON
cat << EOF
{
  "outputs": ["$OUTPUT_NAME"]
}
EOF
