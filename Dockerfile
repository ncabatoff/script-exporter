FROM golang:1.8
WORKDIR /go/src/script-exporter
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -v

FROM docker:17.11
RUN apk add --no-cache bash ca-certificates openssl && update-ca-certificates
WORKDIR /root/
COPY --from=0 /go/src/script-exporter/script-exporter script-exporter.sh
RUN chmod +x script-exporter.sh

CMD /root/script-exporter.sh -script.path /root/scripts -web.listen-address :9661
