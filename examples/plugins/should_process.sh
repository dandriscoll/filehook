#!/bin/bash
# Should-process plugin for filehook
#
# Input: JSON via stdin with:
#   - input: input file path
#   - outputs: array of output paths
#   - input_mtime: input file modification time
#   - output_mtimes: map of output path -> mtime (null if missing)
#
# Output: JSON with:
#   - process: boolean
#   - reason: optional explanation

# Read JSON from stdin
INPUT_JSON=$(cat)

# Extract values using basic parsing (in real use, consider jq)
INPUT_PATH=$(echo "$INPUT_JSON" | grep -o '"input"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/')

# Check if any output is missing (null in output_mtimes)
if echo "$INPUT_JSON" | grep -q '"output_mtimes".*null'; then
    cat << EOF
{
  "process": true,
  "reason": "output missing"
}
EOF
else
    cat << EOF
{
  "process": false,
  "reason": "outputs exist"
}
EOF
fi
