#!/bin/bash
# Generate agent client certificate for mTLS authentication
# Usage: ./generate-agent-cert.sh <agent-id> [output-dir]

set -e

AGENT_ID="${1:?Agent ID required}"
OUTPUT_DIR="${2:-./agent-certs}"
CERTS_DIR="${3:-./certs}"
CERT_DAYS=365  # 1 year validity

# Validate CA exists
if [ ! -f "$CERTS_DIR/ca.crt" ] || [ ! -f "$CERTS_DIR/ca.key" ]; then
    echo "Error: CA certificate not found. Run generate-ca.sh first."
    exit 1
fi

mkdir -p "$OUTPUT_DIR"

AGENT_KEY="$OUTPUT_DIR/$AGENT_ID.key"
AGENT_CSR="$OUTPUT_DIR/$AGENT_ID.csr"
AGENT_CRT="$OUTPUT_DIR/$AGENT_ID.crt"
AGENT_P12="$OUTPUT_DIR/$AGENT_ID.p12"

echo "Generating certificate for agent: $AGENT_ID"

# Generate agent private key (no passphrase for automated use)
openssl genrsa -out "$AGENT_KEY" 2048

# Generate certificate signing request
openssl req -new \
    -key "$AGENT_KEY" \
    -out "$AGENT_CSR" \
    -subj "/C=US/ST=Security/L=Sentinel/O=Sentinel RMM/OU=Agents/CN=$AGENT_ID"

# Create extensions file for agent certificate
cat > "$OUTPUT_DIR/$AGENT_ID.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = $AGENT_ID
EOF

# Sign the certificate with CA
openssl x509 -req \
    -in "$AGENT_CSR" \
    -CA "$CERTS_DIR/ca.crt" \
    -CAkey "$CERTS_DIR/ca.key" \
    -CAcreateserial \
    -out "$AGENT_CRT" \
    -days $CERT_DAYS \
    -sha256 \
    -extfile "$OUTPUT_DIR/$AGENT_ID.ext"

# Create PKCS12 bundle (useful for some clients)
openssl pkcs12 -export \
    -out "$AGENT_P12" \
    -inkey "$AGENT_KEY" \
    -in "$AGENT_CRT" \
    -certfile "$CERTS_DIR/ca.crt" \
    -passout pass:

# Generate certificate fingerprint
FINGERPRINT=$(openssl x509 -in "$AGENT_CRT" -noout -fingerprint -sha256 | sed 's/://g' | cut -d'=' -f2)

# Clean up temporary files
rm -f "$AGENT_CSR" "$OUTPUT_DIR/$AGENT_ID.ext"

# Set permissions
chmod 600 "$AGENT_KEY" "$AGENT_P12"
chmod 644 "$AGENT_CRT"

echo ""
echo "Agent certificate generated successfully!"
echo "  Certificate: $AGENT_CRT"
echo "  Private Key: $AGENT_KEY"
echo "  PKCS12 Bundle: $AGENT_P12"
echo "  Fingerprint: $FINGERPRINT"
echo ""
echo "Certificate valid for $CERT_DAYS days"

# Output fingerprint for database storage
echo "$FINGERPRINT" > "$OUTPUT_DIR/$AGENT_ID.fingerprint"
