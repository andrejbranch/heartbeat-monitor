FROM golang:1.15 as build
WORKDIR $GOPATH/heartbeat-monitor
COPY . .
RUN GO111MODULE=on CGO_ENABLED=0 GOOS=linux go build -o=/bin/heartbeat-monitor ./cmd

FROM scratch
COPY --from=build /bin/heartbeat-monitor /bin/heartbeat-monitor
EXPOSE 9957
ENTRYPOINT ["/bin/heartbeat-monitor"]
