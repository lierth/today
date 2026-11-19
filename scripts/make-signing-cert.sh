#!/bin/sh
# Create the self-signed code-signing certificate the release uses.
#
#   scripts/make-signing-cert.sh [CN]
#
# Default CN is "Today Self-Signed". The script:
#   1. writes a 10-year self-signed certificate with the codeSigning EKU;
#   2. exports a .p12 and imports it into the login keychain;
#   3. marks the certificate as trusted for code signing (this asks for your
#      login password: macOS will not change trust from a script otherwise);
#   4. writes the base64 .p12 to a 0600 file for the GitHub secrets.
#
# The private key is never printed to the terminal and never written into the
# repository. Delete the work directory afterwards if you do not want a second
# copy of the key on disk.
set -eu
cd "$(dirname "$0")/.."

CN="${1:-Today Self-Signed}"
WORK="${TMPDIR:-/tmp}/today-signing"

# The p12 password arrives through the environment or a 0600 file, never as an
# argument: an argument ends up in the process list and in the shell history.
if [ -n "${TODAY_CODESIGN_PASSWORD_FILE:-}" ]; then
  P12_PASS="$(cat "$TODAY_CODESIGN_PASSWORD_FILE")"
elif [ -n "${TODAY_CODESIGN_PASSWORD:-}" ]; then
  P12_PASS="$TODAY_CODESIGN_PASSWORD"
else
  P12_PASS="$(openssl rand -base64 24 | tr -d '/+=' | head -c 24)"
  echo "generated a p12 password; it is written to $WORK/ci-secrets.txt"
fi
if [ -z "$P12_PASS" ]; then
  echo "TODAY_CODESIGN_PASSWORD is empty" >&2
  exit 1
fi

mkdir -p "$WORK"
chmod 700 "$WORK"

# Re-running must not mint a second certificate with the same name: two
# identities for one bundle makes the designated-requirement check depend on
# whichever the keychain answers first.
if security find-certificate -c "$CN" "$HOME/Library/Keychains/login.keychain-db" >/dev/null 2>&1; then
  echo "a certificate named '$CN' is already in the login keychain." >&2
  echo "Delete it first (Keychain Access -> My Certificates), or pass another name." >&2
  exit 1
fi

echo "== generating a self-signed code-signing certificate: $CN"
openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
  -keyout "$WORK/key.pem" -out "$WORK/cert.pem" \
  -subj "/CN=$CN" \
  -addext "basicConstraints=critical,CA:false" \
  -addext "keyUsage=critical,digitalSignature" \
  -addext "extendedKeyUsage=critical,codeSigning"

echo "== exporting a .p12"
# The password goes through a 0600 temp file, not the command line: an
# argument would be visible in `ps` while openssl runs.
printf '%s' "$P12_PASS" > "$WORK/p12pass.txt"
chmod 600 "$WORK/p12pass.txt"
openssl pkcs12 -export -legacy -inkey "$WORK/key.pem" -in "$WORK/cert.pem" \
  -name "$CN" -out "$WORK/today.p12" -passout "file:$WORK/p12pass.txt" 2>/dev/null ||
openssl pkcs12 -export -inkey "$WORK/key.pem" -in "$WORK/cert.pem" \
  -name "$CN" -out "$WORK/today.p12" -passout "file:$WORK/p12pass.txt"

echo "== importing into the login keychain"
# -T codesign: let codesign use the key without a prompt per build.
security import "$WORK/today.p12" -k "$HOME/Library/Keychains/login.keychain-db" \
  -P "$P12_PASS" -T /usr/bin/codesign -T /usr/bin/security

echo "== trusting the certificate for code signing"
echo "   (macOS will ask for your login password; that prompt cannot be skipped)"
security add-trusted-cert -d -r trustRoot -p codeSign \
  -k "$HOME/Library/Keychains/login.keychain-db" "$WORK/cert.pem"

SECRETS="$WORK/ci-secrets.txt"
{
  echo "TODAY_CODESIGN_P12=$(base64 < "$WORK/today.p12" | tr -d '\n')"
  echo "TODAY_CODESIGN_PASSWORD=$P12_PASS"
  echo "CODESIGN_IDENTITY=$CN"
} > "$SECRETS"
chmod 600 "$SECRETS"

cat <<EOF

== done

  Certificate CN:  $CN
  Keychain:        login
  CI secrets file: $SECRETS  (mode 0600)

  Paste the three values from that file into the repository secrets:
    TODAY_CODESIGN_P12       the base64 .p12
    TODAY_CODESIGN_PASSWORD  the p12 password
    CODESIGN_IDENTITY        a plain secret too, so the workflow names it

  The private key also lives in $WORK/key.pem. Shred the directory when
  you are done with it:

    rm -rf "$WORK"

  Nothing from this directory was committed or printed here.
EOF
