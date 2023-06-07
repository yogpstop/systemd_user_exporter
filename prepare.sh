podman create -l "io.containers.autoupdate=registry" \
  --network=none --name="systemd-exporter" --pid=host --userns=keep-id \
  --user=%U -v "/run/user/$(id -u)/bus:/run/user/$(id -u)/bus" \
  --entrypoint=/bin/sh docker.io/yogpstop/systemd-exporter:main \
  -c 'export LISTEN_PID=$$;exec /bin/systemd_exporter --web.systemd-socket --systemd.collector.user --systemd.collector.disable-cgroup-metrics'
podman generate systemd --files --name --new --restart-policy=always \
  --requires=dbus.service --after=dbus.service systemd-exporter

sed -i "s:/run/user/[0-9]\+:/run/user/%U:g;s/%%U/%U/g" container-systemd-exporter.service
sudo install -m644 -vZ container-systemd-exporter.service /etc/systemd/user/
rm -v container-systemd-exporter.service
sudo tee /etc/systemd/user/container-systemd-exporter.socket >/dev/null \
  <<<$'[Socket]\nListenStream=%t/systemd_exporter.sock\n\n[Install]\nWantedBy=sockets.target'
sudo systemctl --global enable container-systemd-exporter.socket
while read -r uid _ _ ; do
  sudo machinectl --uid="$uid" shell .host /bin/systemctl --user start container-systemd-exporter.socket </dev/null
done < <(loginctl list-users --no-legend)
