import os

# Fix server Dockerfile
server_dockerfile = 'D:/Projects/Sentinel/server/Dockerfile'

with open(server_dockerfile, 'r', encoding='utf-8') as f:
    content = f.read()

# Check if already fixed
if 'adduser' not in content:
    content = content.replace(
        '''# Final stage
FROM alpine:3.19

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/sentinel .

# Copy migrations
COPY --from=builder /app/pkg/database/migrations ./pkg/database/migrations

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./sentinel"]''',
        '''# Final stage
FROM alpine:3.19

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \\
    adduser -u 1001 -S appuser -G appgroup

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/sentinel .

# Copy migrations
COPY --from=builder /app/pkg/database/migrations ./pkg/database/migrations

# Set ownership
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./sentinel"]'''
    )

    with open(server_dockerfile, 'w', encoding='utf-8') as f:
        f.write(content)
    print('Fixed server Dockerfile - added non-root user')
else:
    print('Server Dockerfile already has non-root user')

# Fix frontend Dockerfile
frontend_dockerfile = 'D:/Projects/Sentinel/frontend/Dockerfile'

with open(frontend_dockerfile, 'r', encoding='utf-8') as f:
    content = f.read()

# Check if already fixed
if 'USER nginx' not in content:
    content = content.replace(
        '''# Production stage
FROM nginx:alpine

# Copy custom nginx config
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy built assets from builder
COPY --from=builder /app/dist /usr/share/nginx/html

# Expose port
EXPOSE 80

# Start nginx
CMD ["nginx", "-g", "daemon off;"]''',
        '''# Production stage
FROM nginx:alpine

# Copy custom nginx config
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy built assets from builder
COPY --from=builder /app/dist /usr/share/nginx/html

# Create nginx cache directories with proper permissions
RUN mkdir -p /var/cache/nginx /var/run && \\
    chown -R nginx:nginx /var/cache/nginx /var/run /usr/share/nginx/html

# Switch to non-root user
USER nginx

# Expose port
EXPOSE 80

# Start nginx
CMD ["nginx", "-g", "daemon off;"]'''
    )

    with open(frontend_dockerfile, 'w', encoding='utf-8') as f:
        f.write(content)
    print('Fixed frontend Dockerfile - added non-root user')
else:
    print('Frontend Dockerfile already has non-root user')

# Fix docker-compose.yml - add resource limits for frontend
compose_file = 'D:/Projects/Sentinel/docker-compose.yml'

with open(compose_file, 'r', encoding='utf-8') as f:
    content = f.read()

# Add resource limits for frontend if not present
if 'frontend:' in content and 'deploy:' not in content.split('frontend:')[1].split('postgres:')[0]:
    # Find and update frontend service
    content = content.replace(
        '''      - "traefik.http.routers.api.priority=2"
      - "traefik.http.routers.ws.priority=2"

  # PostgreSQL Database''',
        '''      - "traefik.http.routers.api.priority=2"
      - "traefik.http.routers.ws.priority=2"
    deploy:
      resources:
        limits:
          cpus: '1'
          memory: 256M
        reservations:
          cpus: '0.25'
          memory: 64M

  # PostgreSQL Database'''
    )

    with open(compose_file, 'w', encoding='utf-8') as f:
        f.write(content)
    print('Fixed docker-compose.yml - added frontend resource limits')
else:
    print('docker-compose.yml already has frontend resource limits or structure differs')

# Add resource limits and user for traefik if not present
with open(compose_file, 'r', encoding='utf-8') as f:
    content = f.read()

if 'traefik:' in content and 'deploy:' not in content.split('traefik:')[1].split('backend:')[0]:
    content = content.replace(
        '''      - "--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json"

  # Backend API Server''',
        '''      - "--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json"
    deploy:
      resources:
        limits:
          cpus: '1'
          memory: 256M
        reservations:
          cpus: '0.25'
          memory: 64M

  # Backend API Server'''
    )

    with open(compose_file, 'w', encoding='utf-8') as f:
        f.write(content)
    print('Fixed docker-compose.yml - added traefik resource limits')
else:
    print('docker-compose.yml already has traefik resource limits or structure differs')

print('Docker security fixes complete!')
