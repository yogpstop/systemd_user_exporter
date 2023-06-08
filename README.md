# Prepare per-user systemd_exporter
```shell
### Create template container and export systemd service file
# --systemd.collector.disable-cgroup-metrics is not supported by official build
podman create -l "io.containers.autoupdate=registry" \
  --network=none --name="systemd-exporter" --pid=host --userns=keep-id \
  --user=%U -v "/run/user/$(id -u)/bus:/run/user/$(id -u)/bus" \
  --entrypoint=/bin/sh docker.io/yogpstop/systemd-exporter:main \
  -c 'export LISTEN_PID=$$;exec systemd_exporter --web.systemd-socket --systemd.collector.user --systemd.collector.disable-cgroup-metrics'
podman generate systemd --files --name --new --restart-policy=always \
  --requires=dbus.service --after=dbus.service systemd-exporter

### Generalize service file and create socket file
sed -i "s:/run/user/[0-9]\+:/run/user/%U:g;s/%%U/%U/g" container-systemd-exporter.service
sudo install -m644 -vZ container-systemd-exporter.service /etc/systemd/user/
rm -v container-systemd-exporter.service
sudo tee /etc/systemd/user/container-systemd-exporter.socket >/dev/null \
  <<<$'[Socket]\nListenStream=%t/systemd_exporter.sock\n\n[Install]\nWantedBy=sockets.target'
sudo systemctl --global enable container-systemd-exporter.socket

### Start above service for current active users
while read -r uid _ _ ; do
  sudo machinectl --uid="$uid" shell .host /bin/systemctl --user start container-systemd-exporter.socket </dev/null
done < <(loginctl list-users --no-legend)
```

# Prepare system-wide systemd_exporter

```shell
podman create -l "io.containers.autoupdate=registry" \
  --net=none --pid=host --name=systemd-exporter1 \
  -v /var/run/dbus/system_bus_socket:/var/run/dbus/system_bus_socket \
  --entrypoint=/bin/sh docker.io/yogpstop/systemd-exporter:main \
  -c 'export LISTEN_PID=$$;exec systemd_exporter --web.systemd-socket --systemd.collector.disable-cgroup-metrics'
podman generate systemd --files --name --new --restart-policy=always \
  --requires=dbus.service --after=dbus.service systemd-exporter1
install -m644 -vZ container-systemd-exporter1.service .config/systemd/user/
rm -v container-systemd-exporter1.service
tee .config/systemd/user/container-systemd-exporter1.socket >/dev/null <<E
[Socket]
ListenStream=/tmp/systemd_exporter/systemd_exporter.sock
DirectoryMode=0700

[Install]
WantedBy=sockets.target
E
systemctl --user enable --now container-systemd-exporter1.socket
```

# Install aggregate exporter (this project)
```shell
sudo podman create -l "io.containers.autoupdate=registry" \
  --network=none --name="systemd-user-exporter" --pid=host \
  -v /var/run/dbus/system_bus_socket:/var/run/dbus/system_bus_socket \
  -v /run/user:/run/user --entrypoint=/bin/sh \
  docker.io/yogpstop/systemd-user-exporter \
  -c 'export LISTEN_PID=$$;exec systemd_user_exporter --web.systemd-socket'
sudo podman generate systemd --files --name --new --restart-policy=always \
  --requires=dbus.service --after=dbus.service systemd-user-exporter

sudo install -m644 -vZ container-systemd-user-exporter.service /etc/systemd/system/
sudo rm -v container-systemd-user-exporter.service
sudo tee /etc/systemd/system/container-systemd-user-exporter.socket >/dev/null \
  <<<$'[Socket]\nListenStream=9558\n\n[Install]\nWantedBy=sockets.target'
sudo systemctl enable --now container-systemd-user-exporter.socket
```
