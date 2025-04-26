#!/bin/bash

# This script dumps the content of all files TRACKED by Git (respecting .gitignore)
# into workspace_dump.txt in the current directory, prefixed with a timestamp.

OUTPUT_FILE="workspace_dump.txt"

echo "Dumping TRACKED files to $OUTPUT_FILE..."

# --- Create/Truncate the file and write the timestamp first ---
echo "Dump generated on: $(date)" > "$OUTPUT_FILE"
echo "--- Start of dump ---" >> "$OUTPUT_FILE" # Optional separator
echo "" >> "$OUTPUT_FILE" # Add a blank line

# --- Append the file contents using the loop ---
git ls-files --exclude-standard | while IFS= read -r filename; do
  # Skip trying to dump the output file itself if git ls-files lists it
  if [[ "$filename" == "$OUTPUT_FILE" ]]; then
    continue
  fi

  echo "--- File: $filename ---"
  # Handle potential errors reading a file
  if cat "$filename"; then
    echo # Add newline after content only if cat succeeded
  else
    echo ">>> Error reading file: $filename <<<"
  fi
  echo "--- End: $filename ---"
  echo # Add blank line for separation
done >> "$OUTPUT_FILE" # <--- Use >> to APPEND to the file

echo "Dump complete: $OUTPUT_FILE"