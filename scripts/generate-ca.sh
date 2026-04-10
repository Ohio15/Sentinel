#!/bin/bash
# Generate Sentinel RMM Certificate Authority
# This CA is used to sign agent client certificates for mTLS

set -e

CERTS_DIR="${1:-./certs}"
CA_DAYS=3650  # 10 years
CA_KEY_SIZE=4096

mkdir -p "$CERTS_DIR"
cd "$CERTS_DIR"

echo "Generating Sentinel RMM Certificate Authority..."

# Generate CA private key (encrypted with passphrase)
if [ ! -f ca.key ]; then
    openssl genrsa -aes256 -out ca.key $CA_KEY_SIZE
    echo "CA private key generated: ca.key"
else
    echo "CA private key already exists, skipping..."
fi

# Generate CA certificate
if [ ! -f ca.crt ]; then
    openssl req -new -x509 -days $CA_DAYS \
        -key ca.key \
        -sha256 \
        -out ca.crt \
        -subj "/C=US/ST=Security/L=Sentinel/O=Sentinel RMM/OU=Agent Authentication/CN=Sentinel RMM CA"
    echo "CA certificate generated: ca.crt"
else
    echo "CA certificate already exists, skipping..."
fi

# Generate CA certificate hash for agents to verify
openssl x509 -in ca.crt -noout -fingerprint -sha256 | \
    sed 's/://g' | cut -d'=' -f2 > ca.hash
echo "CA hash: $(cat ca.hash)"

# Set secure permissions
chmod 600 ca.key
chmod 644 ca.crt ca.hash

echo ""
echo "Certificate Authority setup complete!"
echo "  CA Certificate: $CERTS_DIR/ca.crt"
echo "  CA Private Key: $CERTS_DIR/ca.key (keep this secure!)"
echo "  CA Hash:        $CERTS_DIR/ca.hash"
echo ""
echo "Next steps:"
echo "1. Copy ca.crt to certs/ca.crt (mounted into sentinel-agent-gateway for mTLS)"
echo "2. Use generate-agent-cert.sh to create agent certificates"
