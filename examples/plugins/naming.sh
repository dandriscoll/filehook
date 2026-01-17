#!/bin/bash
# Naming plugin for filehook
#
# This plugin handles BOTH filename generation AND ready checks in a single call.
# Returns JSON with:
#   - "outputs": array of output filenames (required)
#   - "ready": boolean indicating if file is ready (optional, defaults to true)
#
# NOTE: This is NOT the same as should_process. The naming plugin checks
# if the external source is ready. The should_process plugin checks whether
# to process based on modification policy (timestamps, output existence, etc).
#
# This example converts input.txt -> input.upper.txt

INPUT_PATH="$1"
BASENAME=$(basename "$INPUT_PATH")
NAME="${BASENAME%.*}"
EXT="${BASENAME##*.}"

# Generate output filename
OUTPUT_NAME="${NAME}.upper.${EXT}"

# Example: check if file is ready (you could check external conditions here)
# For this example, we always return ready=true
READY=true

# Output JSON with both outputs and ready status
cat << EOF
{
  "outputs": ["$OUTPUT_NAME"],
  "ready": $READY
}
EOF
