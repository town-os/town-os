#!/usr/bin/env bash

# IRON RULE: make test-full must always be able to run simultaneously in the
# same repository without conflicting. Nothing else matters more than this.
set -e
. make/lib.sh

# Allocate a random available port using Python and write it to the specified file.
if [ -z "$1" ]; then
  echo "Usage: $0 <filename>"
  exit 1
fi

python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()' > "$1"
substep "Port allocated: $(cat "$1") -> $1"
