# Deploy on AWS EC2

This guide deploys the service to Amazon Linux 2023 with:

- Nginx listening publicly on port 80
- The Go service listening privately on `127.0.0.1:8080`
- `systemd` restarting the application after failures or reboots
- SQLite data stored at `/var/lib/url-shortener/urls.db`

## 1. Launch the instance

In the AWS EC2 console:

1. Choose **Amazon Linux 2023 AMI** (non-minimal).
2. Choose an instance size appropriate for your traffic.
3. Create or select an SSH key pair and download the `.pem` file.
4. Use a persistent EBS root volume.
5. Add these inbound security-group rules:

| Type | Port | Source |
|---|---:|---|
| SSH | 22 | My IP |
| HTTP | 80 | `0.0.0.0/0` and `::/0` |
| HTTPS | 443 | `0.0.0.0/0` and `::/0` |

Do not expose port `8080`; only Nginx should reach the Go application.

Allocate and associate an Elastic IP if the public address must remain stable.

## 2. Connect from PowerShell

```powershell
ssh -i "C:\path\to\your-key.pem" ec2-user@YOUR_PUBLIC_IP
```

If Windows rejects the key permissions, restrict the file so only your user
can read it.

## 3. Install the service

Run these commands on the EC2 instance:

```bash
sudo dnf update -y
sudo dnf install -y git
git clone https://github.com/aadityya4real/Url-shortener-service.git
cd Url-shortener-service
sudo bash deploy/ec2/install.sh "http://YOUR_ELASTIC_IP"
```

Open `http://YOUR_ELASTIC_IP` and create a short URL.

## 4. Check logs and health

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
sudo systemctl status url-shortener
sudo journalctl -u url-shortener -f
sudo systemctl status nginx
```

## 5. Deploy updates

```bash
cd ~/Url-shortener-service
git pull --ff-only
sudo bash deploy/ec2/update.sh
```

The update script runs the tests before replacing and restarting the binary.

## Domain and HTTPS

For a public production service:

1. Point a domain's DNS record to the Elastic IP.
2. Change `BASE_URL` in `/etc/url-shortener.env` to the HTTPS domain.
3. Configure an HTTPS certificate using an Application Load Balancer with
   AWS Certificate Manager, or a certificate-aware reverse proxy.
4. Restart the service with `sudo systemctl restart url-shortener`.

Back up `/var/lib/url-shortener/urls.db` and its WAL files together. For
multiple EC2 instances, replace SQLite with a shared database such as Amazon
RDS for PostgreSQL.
