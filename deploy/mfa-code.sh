#!/bin/sh
# Prints the current 6-digit sign-in code for an authenticator setup key (the one create-admin.sh or -reset-mfa
# printed), like an authenticator app would. For local dev, when there's no phone at hand.
#   deploy/mfa-code.sh SETUP_KEY      (or from backend/: make mfa-code KEY=SETUP_KEY)
set -eu

[ $# -eq 1 ] || { echo "usage: $0 SETUP_KEY"; exit 1; }
python3 -I -c '
import base64, hashlib, hmac, struct, sys, time
key = base64.b32decode(sys.argv[1].replace(" ", "").upper() + "=" * (-len(sys.argv[1].replace(" ", "")) % 8))
h = hmac.new(key, struct.pack(">Q", int(time.time()) // 30), hashlib.sha1).digest()
o = h[-1] & 15
code = (struct.unpack(">I", h[o:o + 4])[0] & 0x7FFFFFFF) % 1000000
print(f"{code:06d}  (changes in {30 - int(time.time()) % 30}s)")
' "$1"
