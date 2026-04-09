# VPS Deployment Guide

Complete guide for deploying REVNS to a VPS (DigitalOcean, AWS EC2, Linode, etc.)

## Prerequisites

- VPS with at least 4GB RAM, 2 CPU cores, 50GB SSD
- Ubuntu 22.04 LTS or similar
- Docker & Docker Compose installed
- Domain name (optional but recommended)
- SSH access to server

## Step 1: Server Setup

### 1.1 Update System
```bash
sudo apt update && sudo apt upgrade -y
```

### 1.2 Install Docker
```bash
# Install Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# Install Docker Compose
sudo curl -L "https://github.com/docker/compose/releases/download/v2.20.0/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose

# Add user to docker group
sudo usermod -aG docker $USER
newgrp docker
```

### 1.3 Configure Firewall
```bash
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw allow 8080/tcp   # API (or restrict to localhost)
sudo ufw --force enable
```

## Step 2: Deploy Application

### 2.1 Clone Repository
```bash
cd ~
git clone <your-repo-url> revns
cd revns
```

### 2.2 Configure Environment
```bash
# Copy environment template
cp .env.example .env

# Edit configuration
nano .env
```

**Minimum required settings:**
```
GRAFANA_PASSWORD=your_secure_password
DOMAIN=your-domain.com
EMAIL=your-email@example.com
```

### 2.3 Run Deployment
```bash
./deploy.sh
```

This will:
- Build Docker images
- Start ScyllaDB and Redis
- Initialize database schema
- Start API and Web services
- Run health checks

## Step 3: SSL with Let's Encrypt (Recommended)

### 3.1 Install Certbot
```bash
sudo apt install certbot python3-certbot-nginx -y
```

### 3.2 Obtain Certificate
```bash
sudo certbot --nginx -d your-domain.com -d www.your-domain.com
```

### 3.3 Auto-renewal
```bash
sudo systemctl enable certbot.timer
sudo certbot renew --dry-run
```

## Step 4: Configure Domain

### 4.1 DNS Setup
Point your domain to VPS IP:
```
A record: your-domain.com -> YOUR_VPS_IP
```

### 4.2 Update Nginx Config
Edit `nginx.conf` and change:
```
server_name your-domain.com;
```

Rebuild web container:
```bash
docker-compose -f docker-compose.prod.yml up -d --build web
```

## Step 5: Data Ingestion

### 5.1 Upload CSV File
```bash
# Copy CSV to server
scp domains.csv user@your-vps:/home/user/revns/data/
```

### 5.2 Run Ingester
```bash
# Build ingester
cd ~/revns/api && go build -o ../bin/ingester ./cmd/ingester

# Ingest data
./bin/ingester -csv data/domains.csv -scylla localhost -redis localhost:6379
```

Or use Docker:
```bash
docker run --rm --network revns-network \
  -v $(pwd)/data:/data \
  revns-api:latest \
  ./ingester -csv /data/domains.csv -scylla scylla -redis redis:6379
```

## Step 6: Monitoring & Maintenance

### 6.1 View Logs
```bash
# All services
docker-compose -f docker-compose.prod.yml logs -f

# Specific service
docker-compose -f docker-compose.prod.yml logs -f api
```

### 6.2 Check Status
```bash
docker-compose -f docker-compose.prod.yml ps
```

### 6.3 Restart Services
```bash
# Restart all
docker-compose -f docker-compose.prod.yml restart

# Restart specific service
docker-compose -f docker-compose.prod.yml restart api
```

### 6.4 Update Application
```bash
git pull
./deploy.sh
```

### 6.5 Backup Data
```bash
# Backup script
#!/bin/bash
DATE=$(date +%Y%m%d_%H%M%S)
docker exec revns-scylla nodetool snapshot domain_data
sudo tar czf ~/backups/revns_backup_$DATE.tar.gz /var/lib/docker/volumes/revns_scylla_data/
```

## Step 7: Performance Tuning

### 7.1 ScyllaDB Optimization
For production with high traffic, edit `docker-compose.prod.yml`:
```yaml
scylla:
  command: --smp 4 --memory 8G --overprovisioned 1
```

### 7.2 System Limits
```bash
# Edit /etc/sysctl.conf
sudo sysctl -w fs.aio-max-nr=1048576
```

### 7.3 Docker Resources
```bash
# Edit /etc/docker/daemon.json
{
  "exec-opts": ["native.cgroupdriver=systemd"],
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "100m"
  },
  "storage-driver": "overlay2"
}
```

## Troubleshooting

### API Not Responding
```bash
# Check logs
docker-compose -f docker-compose.prod.yml logs api

# Check health
curl http://localhost:8080/health/live
```

### Database Issues
```bash
# Check ScyllaDB
docker exec revns-scylla nodetool status
docker exec revns-scylla cqlsh -e "DESCRIBE KEYSPACES"
```

### Memory Issues
```bash
# Check memory usage
free -h
docker stats --no-stream
```

### Disk Space
```bash
# Check disk usage
df -h
docker system prune -f  # Clean unused images
```

## Security Recommendations

1. **Use Firewall**: Only expose ports 80, 443, and 22
2. **Enable SSL**: Always use HTTPS in production
3. **Change Defaults**: Update all default passwords
4. **Regular Updates**: Keep system and Docker images updated
5. **Backups**: Schedule regular database backups
6. **Monitoring**: Set up alerts for disk space, memory, CPU

## Access Points

After deployment:
- **Application**: http://your-domain.com or http://YOUR_VPS_IP
- **API Docs**: http://your-domain.com/api/v1
- **Prometheus**: http://your-domain.com:9090
- **Grafana**: http://your-domain.com:3000 (login: admin / password from .env)

## Support

For issues:
1. Check logs: `docker-compose -f docker-compose.prod.yml logs`
2. Health check: `curl http://localhost:8080/health/live`
3. Restart: `docker-compose -f docker-compose.prod.yml restart`
