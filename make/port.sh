#!/usr/bin/env bash
set -e

# Allocate a random available port using Python and write it to the specified file.
if [ -z "$1" ]; then
  echo "Usage: $0 <filename>"
  exit 1
fi

python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()' > "$1"
echo "Port allocated: $(cat "$1") -> $1"
