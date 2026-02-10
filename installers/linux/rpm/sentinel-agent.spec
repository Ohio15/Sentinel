Name:           sentinel-agent
Version:        %%VERSION%%
Release:        1%{?dist}
Summary:        Sentinel RMM Agent

License:        Proprietary
URL:            https://sentinelrmm.us
Source0:        sentinel-agent-linux-amd64
Source1:        sentinel-watchdog-linux-amd64
Source2:        sentinel-agent.service
Source3:        sentinel-watchdog.service

Requires:       systemd
Requires:       ca-certificates

%description
Sentinel is a remote monitoring and management (RMM) agent that provides
system monitoring, remote access, and automated maintenance capabilities.

This package includes:
 - sentinel-agent: Main RMM agent daemon
 - sentinel-watchdog: Process monitor to ensure agent availability

%prep
# No source archive to extract

%build
# Pre-built binaries, no compilation needed

%install
rm -rf %{buildroot}

# Create directories
mkdir -p %{buildroot}/usr/local/bin
mkdir -p %{buildroot}/etc/systemd/system
mkdir -p %{buildroot}/etc/sentinel
mkdir -p %{buildroot}/var/log/sentinel

# Install binaries
install -m 755 %{SOURCE0} %{buildroot}/usr/local/bin/sentinel-agent
install -m 755 %{SOURCE1} %{buildroot}/usr/local/bin/sentinel-watchdog

# Install systemd service files
install -m 644 %{SOURCE2} %{buildroot}/etc/systemd/system/sentinel-agent.service
install -m 644 %{SOURCE3} %{buildroot}/etc/systemd/system/sentinel-watchdog.service

# Install default config
cat > %{buildroot}/etc/sentinel/config.json << 'EOFCONFIG'
{
  "server_url": "%%SERVER_URL%%",
  "grpc_endpoint": "%%GRPC_ENDPOINT%%",
  "enrollment_token": "%%ENROLLMENT_TOKEN%%",
  "organization_id": "%%ORGANIZATION_ID%%"
}
EOFCONFIG

%pre
# Create sentinel group if it doesn't exist
getent group sentinel >/dev/null 2>&1 || groupadd --system sentinel

# Create sentinel user if it doesn't exist
getent passwd sentinel >/dev/null 2>&1 || useradd --system \
    --gid sentinel \
    --home-dir /etc/sentinel \
    --no-create-home \
    --shell /sbin/nologin \
    sentinel

%post
# Set ownership and permissions
chown -R sentinel:sentinel /etc/sentinel
chown -R sentinel:sentinel /var/log/sentinel
chmod 750 /etc/sentinel
chmod 750 /var/log/sentinel
chmod 640 /etc/sentinel/config.json

# Reload systemd
systemctl daemon-reload

# Enable services
systemctl enable sentinel-agent.service
systemctl enable sentinel-watchdog.service

echo ""
echo "=============================================="
echo "Sentinel Agent installed successfully!"
echo "=============================================="
echo ""
echo "Next steps:"
echo "  1. Edit /etc/sentinel/config.json with your server details"
echo "  2. Start the services:"
echo "     sudo systemctl start sentinel-agent"
echo "     sudo systemctl start sentinel-watchdog"
echo ""
echo "View logs:"
echo "  sudo journalctl -u sentinel-agent -f"
echo "  sudo journalctl -u sentinel-watchdog -f"
echo ""

%preun
if [ $1 -eq 0 ]; then
    # Package removal, not upgrade
    systemctl stop sentinel-watchdog.service 2>/dev/null || true
    systemctl stop sentinel-agent.service 2>/dev/null || true
    systemctl disable sentinel-watchdog.service 2>/dev/null || true
    systemctl disable sentinel-agent.service 2>/dev/null || true
fi

%postun
systemctl daemon-reload

if [ $1 -eq 0 ]; then
    # Package removal, not upgrade
    echo "Sentinel Agent removed. Configuration and logs preserved."
    echo "To completely remove config and logs:"
    echo "  sudo rm -rf /etc/sentinel /var/log/sentinel"
    echo "To remove the sentinel user:"
    echo "  sudo userdel sentinel"
fi

%files
%attr(755, root, root) /usr/local/bin/sentinel-agent
%attr(755, root, root) /usr/local/bin/sentinel-watchdog
%attr(644, root, root) /etc/systemd/system/sentinel-agent.service
%attr(644, root, root) /etc/systemd/system/sentinel-watchdog.service
%dir %attr(750, sentinel, sentinel) /etc/sentinel
%config(noreplace) %attr(640, sentinel, sentinel) /etc/sentinel/config.json
%dir %attr(750, sentinel, sentinel) /var/log/sentinel

%changelog
* %(date "+%a %b %d %Y") Sentinel RMM <support@sentinelrmm.us> - %%VERSION%%-1
- Initial package release
