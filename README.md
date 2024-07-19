# Telegram Moderator

## Create Certificate

Create a certificate for your own IP address. Use your own IP address (where the moderator will run) in the `CN` field.

```bash
cd cmd/server/certs
openssl req -newkey rsa:2048 -sha256 -nodes -keyout private.key -x509 -days 365 -out public.pem
```

You'll need to add the public key `public.pem` to the Telegram API post request in the `certificate` field of the `setWebhook` method and keep the private key in the `private.key` file in the `cmd/server/certs` folder.

## Create env file
Create a `.env` file in the `cmd/server` by copying the `.env.example` and renaming it to `.env`. Fill in the values for the `TELEGRAM_BOT_API_TOKEN` and `LOCAL_PORT_FOR_WEBHOOK` fields.

## Build

Go to the server folder (execute the command from the local machine):
```bash
cd cmd/server
```
Create a binary for the server (execute the command from the local machine):
```bash
GOOS=linux GOARCH=amd64 go build -o ../../telegram-moderator
```
Stop the service on remote server if it's running (execute the command from the server):
```bash
sudo systemctl stop telegram-moderator.service
```

Put the binary from local machine to the server via the `scp` command and start the service. Example of `scp` command (execute the command from the local machine):

```bash
scp ../../telegram-moderator scaleway_ubuntu_11:~/proj/telegram-moderator/
```

Start the service on the remote server (execute the command from the server):

```bash
sudo systemctl start telegram-moderator.service
```

## First Deploy

Create a systemd service file (execute this and the following commands from the server):

```bash
sudo nano /etc/systemd/system/telegram-moderator.service
```

Add the following content:

```text
[Unit]
Description=Telegram Moderator Service
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu/proj/telegram-moderator
ExecStart=/home/ubuntu/proj/telegram-moderator/telegram-moderator
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Build the binary with instructions from the previous section.

Put private key to the `cmd/server/certs` folder on the remote server.

Then reload the systemd daemon, enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable telegram-moderator.service
sudo systemctl start telegram-moderator.service
sudo systemctl status telegram-moderator.service
```


