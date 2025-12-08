#!/bin/bash
set -e

# Sentinel RMM Platform - Oracle Cloud Infrastructure Setup
# This script creates the required Oracle Cloud resources for deployment
# Requires: OCI CLI installed and configured

echo "=========================================="
echo "  Sentinel RMM - Oracle Cloud Setup"
echo "=========================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration - Override these with environment variables
COMPARTMENT_ID=${COMPARTMENT_ID:-""}
REGION=${REGION:-"us-ashburn-1"}
AVAILABILITY_DOMAIN=${AVAILABILITY_DOMAIN:-""}
SSH_PUBLIC_KEY_FILE=${SSH_PUBLIC_KEY_FILE:-"$HOME/.ssh/id_rsa.pub"}

# Resource names
VCN_NAME="sentinel-vcn"
SUBNET_NAME="sentinel-subnet"
IGW_NAME="sentinel-igw"
RT_NAME="sentinel-route-table"
SL_NAME="sentinel-security-list"
APP_VM_NAME="sentinel-app"
DB_VM_NAME="sentinel-db"

# Network configuration
VCN_CIDR="10.0.0.0/16"
SUBNET_CIDR="10.0.1.0/24"

# ARM shape for free tier (Ampere A1)
VM_SHAPE="VM.Standard.A1.Flex"
APP_OCPUS=2
APP_MEMORY_GB=12
DB_OCPUS=2
DB_MEMORY_GB=12
BOOT_VOLUME_SIZE_GB=50
DB_BOOT_VOLUME_SIZE_GB=100

# Oracle Linux 9 ARM image (update OCID for your region)
# Find your region's image: https://docs.oracle.com/en-us/iaas/images/
declare -A IMAGE_OCIDS
IMAGE_OCIDS["us-ashburn-1"]="ocid1.image.oc1.iad.aaaaaaaawwax2iqkcrg65cxr7xp6oqbqvp3yzlmxsle3y7xvg2bh3d6fy5ka"
IMAGE_OCIDS["us-phoenix-1"]="ocid1.image.oc1.phx.aaaaaaaay5yw5fg77mwocxbwj5xstcn6rhhlzq4pt7pvcqhna6d5wsrn3gza"
IMAGE_OCIDS["eu-frankfurt-1"]="ocid1.image.oc1.eu-frankfurt-1.aaaaaaaavnzxvw7oqzlgz3vfp6eykxzf2xp2xfj5qzrfvwt7s5x7m7p3z5ua"
IMAGE_OCIDS["uk-london-1"]="ocid1.image.oc1.uk-london-1.aaaaaaaayxjfz7bqvpxlwevoj7i2zv7qnjz7c3kqvxv6l5y3w6x7m7p3z5ua"

# Function to check prerequisites
check_prerequisites() {
    echo -e "${YELLOW}Checking prerequisites...${NC}"

    # Check OCI CLI
    if ! command -v oci &> /dev/null; then
        echo -e "${RED}Error: OCI CLI is not installed${NC}"
        echo "Install it from: https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/cliinstall.htm"
        exit 1
    fi
    echo -e "${GREEN}✓ OCI CLI installed${NC}"

    # Check OCI config
    if ! oci iam region list &> /dev/null; then
        echo -e "${RED}Error: OCI CLI is not configured${NC}"
        echo "Run: oci setup config"
        exit 1
    fi
    echo -e "${GREEN}✓ OCI CLI configured${NC}"

    # Check SSH key
    if [ ! -f "$SSH_PUBLIC_KEY_FILE" ]; then
        echo -e "${RED}Error: SSH public key not found at $SSH_PUBLIC_KEY_FILE${NC}"
        echo "Generate one with: ssh-keygen -t rsa -b 4096"
        exit 1
    fi
    echo -e "${GREEN}✓ SSH public key found${NC}"

    # Get compartment ID if not set
    if [ -z "$COMPARTMENT_ID" ]; then
        echo -e "${YELLOW}Getting tenancy (root compartment) ID...${NC}"
        COMPARTMENT_ID=$(oci iam compartment list --query 'data[0]."compartment-id"' --raw-output 2>/dev/null)
        if [ -z "$COMPARTMENT_ID" ]; then
            COMPARTMENT_ID=$(oci iam tenancy get --query 'data.id' --raw-output)
        fi
    fi
    echo -e "${GREEN}✓ Compartment ID: $COMPARTMENT_ID${NC}"

    # Get availability domain if not set
    if [ -z "$AVAILABILITY_DOMAIN" ]; then
        AVAILABILITY_DOMAIN=$(oci iam availability-domain list --compartment-id "$COMPARTMENT_ID" --query 'data[0].name' --raw-output)
    fi
    echo -e "${GREEN}✓ Availability Domain: $AVAILABILITY_DOMAIN${NC}"
}

# Function to create VCN
create_vcn() {
    echo -e "${YELLOW}Creating Virtual Cloud Network...${NC}"

    # Check if VCN already exists
    EXISTING_VCN=$(oci network vcn list --compartment-id "$COMPARTMENT_ID" --display-name "$VCN_NAME" --query 'data[0].id' --raw-output 2>/dev/null || true)

    if [ -n "$EXISTING_VCN" ] && [ "$EXISTING_VCN" != "null" ]; then
        echo -e "${BLUE}VCN already exists: $EXISTING_VCN${NC}"
        VCN_ID=$EXISTING_VCN
    else
        VCN_ID=$(oci network vcn create \
            --compartment-id "$COMPARTMENT_ID" \
            --display-name "$VCN_NAME" \
            --cidr-blocks "[\"$VCN_CIDR\"]" \
            --dns-label "sentinel" \
            --query 'data.id' --raw-output)
        echo -e "${GREEN}✓ VCN created: $VCN_ID${NC}"
    fi
}

# Function to create Internet Gateway
create_internet_gateway() {
    echo -e "${YELLOW}Creating Internet Gateway...${NC}"

    EXISTING_IGW=$(oci network internet-gateway list --compartment-id "$COMPARTMENT_ID" --vcn-id "$VCN_ID" --display-name "$IGW_NAME" --query 'data[0].id' --raw-output 2>/dev/null || true)

    if [ -n "$EXISTING_IGW" ] && [ "$EXISTING_IGW" != "null" ]; then
        echo -e "${BLUE}Internet Gateway already exists: $EXISTING_IGW${NC}"
        IGW_ID=$EXISTING_IGW
    else
        IGW_ID=$(oci network internet-gateway create \
            --compartment-id "$COMPARTMENT_ID" \
            --vcn-id "$VCN_ID" \
            --display-name "$IGW_NAME" \
            --is-enabled true \
            --query 'data.id' --raw-output)
        echo -e "${GREEN}✓ Internet Gateway created: $IGW_ID${NC}"
    fi
}

# Function to create Route Table
create_route_table() {
    echo -e "${YELLOW}Creating Route Table...${NC}"

    EXISTING_RT=$(oci network route-table list --compartment-id "$COMPARTMENT_ID" --vcn-id "$VCN_ID" --display-name "$RT_NAME" --query 'data[0].id' --raw-output 2>/dev/null || true)

    if [ -n "$EXISTING_RT" ] && [ "$EXISTING_RT" != "null" ]; then
        echo -e "${BLUE}Route Table already exists: $EXISTING_RT${NC}"
        RT_ID=$EXISTING_RT
    else
        RT_ID=$(oci network route-table create \
            --compartment-id "$COMPARTMENT_ID" \
            --vcn-id "$VCN_ID" \
            --display-name "$RT_NAME" \
            --route-rules "[{\"destination\":\"0.0.0.0/0\",\"destinationType\":\"CIDR_BLOCK\",\"networkEntityId\":\"$IGW_ID\"}]" \
            --query 'data.id' --raw-output)
        echo -e "${GREEN}✓ Route Table created: $RT_ID${NC}"
    fi
}

# Function to create Security List
create_security_list() {
    echo -e "${YELLOW}Creating Security List...${NC}"

    EXISTING_SL=$(oci network security-list list --compartment-id "$COMPARTMENT_ID" --vcn-id "$VCN_ID" --display-name "$SL_NAME" --query 'data[0].id' --raw-output 2>/dev/null || true)

    if [ -n "$EXISTING_SL" ] && [ "$EXISTING_SL" != "null" ]; then
        echo -e "${BLUE}Security List already exists: $EXISTING_SL${NC}"
        SL_ID=$EXISTING_SL
    else
        # Security rules JSON
        INGRESS_RULES='[
            {"protocol":"6","source":"0.0.0.0/0","tcpOptions":{"destinationPortRange":{"min":22,"max":22}},"description":"SSH"},
            {"protocol":"6","source":"0.0.0.0/0","tcpOptions":{"destinationPortRange":{"min":80,"max":80}},"description":"HTTP"},
            {"protocol":"6","source":"0.0.0.0/0","tcpOptions":{"destinationPortRange":{"min":443,"max":443}},"description":"HTTPS"},
            {"protocol":"all","source":"10.0.0.0/16","description":"Internal VCN traffic"}
        ]'

        EGRESS_RULES='[
            {"protocol":"all","destination":"0.0.0.0/0","description":"Allow all outbound"}
        ]'

        SL_ID=$(oci network security-list create \
            --compartment-id "$COMPARTMENT_ID" \
            --vcn-id "$VCN_ID" \
            --display-name "$SL_NAME" \
            --ingress-security-rules "$INGRESS_RULES" \
            --egress-security-rules "$EGRESS_RULES" \
            --query 'data.id' --raw-output)
        echo -e "${GREEN}✓ Security List created: $SL_ID${NC}"
    fi
}

# Function to create Subnet
create_subnet() {
    echo -e "${YELLOW}Creating Subnet...${NC}"

    EXISTING_SUBNET=$(oci network subnet list --compartment-id "$COMPARTMENT_ID" --vcn-id "$VCN_ID" --display-name "$SUBNET_NAME" --query 'data[0].id' --raw-output 2>/dev/null || true)

    if [ -n "$EXISTING_SUBNET" ] && [ "$EXISTING_SUBNET" != "null" ]; then
        echo -e "${BLUE}Subnet already exists: $EXISTING_SUBNET${NC}"
        SUBNET_ID=$EXISTING_SUBNET
    else
        SUBNET_ID=$(oci network subnet create \
            --compartment-id "$COMPARTMENT_ID" \
            --vcn-id "$VCN_ID" \
            --display-name "$SUBNET_NAME" \
            --cidr-block "$SUBNET_CIDR" \
            --route-table-id "$RT_ID" \
            --security-list-ids "[\"$SL_ID\"]" \
            --dns-label "subnet1" \
            --query 'data.id' --raw-output)
        echo -e "${GREEN}✓ Subnet created: $SUBNET_ID${NC}"
    fi
}

# Function to create cloud-init script for App VM
create_app_cloud_init() {
    cat << 'CLOUD_INIT'
#!/bin/bash
# Update system
dnf update -y

# Install Docker
dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
dnf install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Start Docker
systemctl enable docker
systemctl start docker

# Install Git
dnf install -y git

# Create sentinel user
useradd -m -s /bin/bash sentinel
usermod -aG docker sentinel

# Create application directory
mkdir -p /opt/sentinel
chown sentinel:sentinel /opt/sentinel

echo "Cloud-init completed for App VM"
CLOUD_INIT
}

# Function to create cloud-init script for DB VM
create_db_cloud_init() {
    cat << 'CLOUD_INIT'
#!/bin/bash
# Update system
dnf update -y

# Install Docker
dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
dnf install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Start Docker
systemctl enable docker
systemctl start docker

# Create sentinel user
useradd -m -s /bin/bash sentinel
usermod -aG docker sentinel

# Create data directories
mkdir -p /opt/sentinel/data/postgres
mkdir -p /opt/sentinel/data/redis
chown -R sentinel:sentinel /opt/sentinel

echo "Cloud-init completed for DB VM"
CLOUD_INIT
}

# Function to create VM
create_vm() {
    local VM_NAME=$1
    local OCPUS=$2
    local MEMORY_GB=$3
    local BOOT_SIZE_GB=$4
    local CLOUD_INIT_SCRIPT=$5
    local ASSIGN_PUBLIC_IP=$6

    echo -e "${YELLOW}Creating VM: $VM_NAME...${NC}"

    # Check if VM already exists
    EXISTING_VM=$(oci compute instance list --compartment-id "$COMPARTMENT_ID" --display-name "$VM_NAME" --lifecycle-state RUNNING --query 'data[0].id' --raw-output 2>/dev/null || true)

    if [ -n "$EXISTING_VM" ] && [ "$EXISTING_VM" != "null" ]; then
        echo -e "${BLUE}VM already exists: $EXISTING_VM${NC}"
        return
    fi

    # Get image OCID for region
    IMAGE_ID=${IMAGE_OCIDS[$REGION]}
    if [ -z "$IMAGE_ID" ]; then
        echo -e "${RED}Error: No image OCID configured for region $REGION${NC}"
        echo "Please add the Oracle Linux 9 ARM image OCID for your region"
        exit 1
    fi

    # Read SSH public key
    SSH_KEY=$(cat "$SSH_PUBLIC_KEY_FILE")

    # Create cloud-init file
    CLOUD_INIT_FILE=$(mktemp)
    echo "$CLOUD_INIT_SCRIPT" > "$CLOUD_INIT_FILE"
    CLOUD_INIT_BASE64=$(base64 -w 0 "$CLOUD_INIT_FILE")
    rm "$CLOUD_INIT_FILE"

    # Determine VNIC config based on public IP requirement
    if [ "$ASSIGN_PUBLIC_IP" = "true" ]; then
        VNIC_DETAILS="{\"subnetId\":\"$SUBNET_ID\",\"assignPublicIp\":true}"
    else
        VNIC_DETAILS="{\"subnetId\":\"$SUBNET_ID\",\"assignPublicIp\":false}"
    fi

    # Create instance
    INSTANCE_ID=$(oci compute instance launch \
        --compartment-id "$COMPARTMENT_ID" \
        --availability-domain "$AVAILABILITY_DOMAIN" \
        --display-name "$VM_NAME" \
        --shape "$VM_SHAPE" \
        --shape-config "{\"ocpus\":$OCPUS,\"memoryInGBs\":$MEMORY_GB}" \
        --image-id "$IMAGE_ID" \
        --create-vnic-details "$VNIC_DETAILS" \
        --metadata "{\"ssh_authorized_keys\":\"$SSH_KEY\",\"user_data\":\"$CLOUD_INIT_BASE64\"}" \
        --source-details "{\"sourceType\":\"image\",\"imageId\":\"$IMAGE_ID\",\"bootVolumeSizeInGBs\":$BOOT_SIZE_GB}" \
        --query 'data.id' --raw-output)

    echo -e "${GREEN}✓ VM created: $INSTANCE_ID${NC}"

    # Wait for instance to be running
    echo -e "${YELLOW}Waiting for VM to be running...${NC}"
    oci compute instance get --instance-id "$INSTANCE_ID" --wait-for-state RUNNING --wait-interval-seconds 10 > /dev/null
    echo -e "${GREEN}✓ VM is running${NC}"

    # Get public IP if assigned
    if [ "$ASSIGN_PUBLIC_IP" = "true" ]; then
        sleep 10  # Wait for IP assignment
        PUBLIC_IP=$(oci compute instance list-vnics --instance-id "$INSTANCE_ID" --query 'data[0]."public-ip"' --raw-output)
        echo -e "${GREEN}✓ Public IP: $PUBLIC_IP${NC}"
    fi
}

# Function to get VM IPs
get_vm_ips() {
    echo -e "${YELLOW}Getting VM IP addresses...${NC}"

    # Get App VM IP
    APP_INSTANCE_ID=$(oci compute instance list --compartment-id "$COMPARTMENT_ID" --display-name "$APP_VM_NAME" --lifecycle-state RUNNING --query 'data[0].id' --raw-output)
    if [ -n "$APP_INSTANCE_ID" ] && [ "$APP_INSTANCE_ID" != "null" ]; then
        APP_PUBLIC_IP=$(oci compute instance list-vnics --instance-id "$APP_INSTANCE_ID" --query 'data[0]."public-ip"' --raw-output)
        APP_PRIVATE_IP=$(oci compute instance list-vnics --instance-id "$APP_INSTANCE_ID" --query 'data[0]."private-ip"' --raw-output)
        echo -e "${GREEN}App VM - Public: $APP_PUBLIC_IP, Private: $APP_PRIVATE_IP${NC}"
    fi

    # Get DB VM IP
    DB_INSTANCE_ID=$(oci compute instance list --compartment-id "$COMPARTMENT_ID" --display-name "$DB_VM_NAME" --lifecycle-state RUNNING --query 'data[0].id' --raw-output)
    if [ -n "$DB_INSTANCE_ID" ] && [ "$DB_INSTANCE_ID" != "null" ]; then
        DB_PRIVATE_IP=$(oci compute instance list-vnics --instance-id "$DB_INSTANCE_ID" --query 'data[0]."private-ip"' --raw-output)
        echo -e "${GREEN}DB VM - Private: $DB_PRIVATE_IP${NC}"
    fi
}

# Function to print summary
print_summary() {
    echo ""
    echo "=========================================="
    echo -e "${GREEN}  Oracle Cloud Setup Complete!${NC}"
    echo "=========================================="
    echo ""
    echo "Resources created:"
    echo "  - VCN: $VCN_NAME ($VCN_CIDR)"
    echo "  - Subnet: $SUBNET_NAME ($SUBNET_CIDR)"
    echo "  - Internet Gateway: $IGW_NAME"
    echo "  - Security List: $SL_NAME"
    echo "  - App VM: $APP_VM_NAME (${APP_OCPUS} OCPUs, ${APP_MEMORY_GB}GB RAM)"
    echo "  - DB VM: $DB_VM_NAME (${DB_OCPUS} OCPUs, ${DB_MEMORY_GB}GB RAM)"
    echo ""

    if [ -n "$APP_PUBLIC_IP" ]; then
        echo "Next steps:"
        echo ""
        echo "1. SSH to App VM:"
        echo "   ssh opc@$APP_PUBLIC_IP"
        echo ""
        echo "2. Clone the repository:"
        echo "   git clone https://github.com/yourusername/sentinel.git /opt/sentinel"
        echo ""
        echo "3. Run the deployment script:"
        echo "   cd /opt/sentinel"
        echo "   sudo DOMAIN=your-domain.com EMAIL=your@email.com ./scripts/deploy.sh"
        echo ""
        echo "4. Configure DNS:"
        echo "   Point your domain to: $APP_PUBLIC_IP"
        echo ""
    fi

    # Save configuration to file
    CONFIG_FILE="oracle-setup-output.env"
    cat > "$CONFIG_FILE" << EOF
# Oracle Cloud Setup Output
# Generated: $(date)

VCN_ID=$VCN_ID
SUBNET_ID=$SUBNET_ID
IGW_ID=$IGW_ID
RT_ID=$RT_ID
SL_ID=$SL_ID
APP_PUBLIC_IP=$APP_PUBLIC_IP
APP_PRIVATE_IP=$APP_PRIVATE_IP
DB_PRIVATE_IP=$DB_PRIVATE_IP
EOF
    echo "Configuration saved to: $CONFIG_FILE"
}

# Main function
main() {
    echo ""
    echo -e "${BLUE}Region: $REGION${NC}"
    echo ""

    check_prerequisites

    echo ""
    echo -e "${YELLOW}Creating network infrastructure...${NC}"
    create_vcn
    create_internet_gateway
    create_route_table
    create_security_list
    create_subnet

    echo ""
    echo -e "${YELLOW}Creating virtual machines...${NC}"

    # Create App VM with public IP
    APP_CLOUD_INIT=$(create_app_cloud_init)
    create_vm "$APP_VM_NAME" "$APP_OCPUS" "$APP_MEMORY_GB" "$BOOT_VOLUME_SIZE_GB" "$APP_CLOUD_INIT" "true"

    # Create DB VM without public IP (internal only)
    DB_CLOUD_INIT=$(create_db_cloud_init)
    create_vm "$DB_VM_NAME" "$DB_OCPUS" "$DB_MEMORY_GB" "$DB_BOOT_VOLUME_SIZE_GB" "$DB_CLOUD_INIT" "false"

    echo ""
    get_vm_ips
    print_summary
}

# Help function
show_help() {
    echo "Sentinel RMM - Oracle Cloud Setup Script"
    echo ""
    echo "Usage: $0 [options]"
    echo ""
    echo "Environment variables:"
    echo "  COMPARTMENT_ID        OCI Compartment ID (default: tenancy root)"
    echo "  REGION                OCI Region (default: us-ashburn-1)"
    echo "  AVAILABILITY_DOMAIN   Availability Domain (default: first available)"
    echo "  SSH_PUBLIC_KEY_FILE   Path to SSH public key (default: ~/.ssh/id_rsa.pub)"
    echo ""
    echo "Example:"
    echo "  REGION=us-phoenix-1 ./oracle-setup.sh"
    echo ""
}

# Parse arguments
case "$1" in
    -h|--help)
        show_help
        exit 0
        ;;
    *)
        main "$@"
        ;;
esac
