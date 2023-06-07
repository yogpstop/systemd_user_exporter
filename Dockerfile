FROM docker.io/golang:alpine AS build
WORKDIR /app
COPY . /app
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w"

FROM scratch
COPY --from=build /app/systemd_user_exporter /bin/systemd_user_exporter
EXPOSE 9558
#VOLUME /var/run/dbus/system_bus_socket /run/user
ENTRYPOINT ["/bin/systemd_user_exporter"]
