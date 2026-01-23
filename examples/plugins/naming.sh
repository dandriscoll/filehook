#!/bin/bash
# Naming plugin for filehook (ft tool contract)
#
# Operations:
#   ready <path>           - Returns "true" or "false" (is file ready?)
#   propose <path> <type>  - Returns the proposed output filename
#
# NOTE: The "ready" check is NOT the same as should_process. The ready check
# determines if the external source is ready for transformation. The
# should_process plugin checks whether to process based on modification policy.
#
# This example converts input.txt -> input.upper.txt

OPERATION="$1"
INPUT_PATH="$2"
TARGET_TYPE="$3"

case "$OPERATION" in
  ready)
    # Check if file is ready for transformation
    # For this example, we always return true
    echo "true"
    ;;

  propose)
    # Generate output filename based on input path and target type
    BASENAME=$(basename "$INPUT_PATH")
    DIRNAME=$(dirname "$INPUT_PATH")
    NAME="${BASENAME%.*}"
    EXT="${BASENAME##*.}"

    # Propose output filename (includes directory from input)
    echo "${DIRNAME}/${NAME}.${TARGET_TYPE}.${EXT}"
    ;;

  *)
    echo "Unknown operation: $OPERATION" >&2
    exit 1
    ;;
esac
