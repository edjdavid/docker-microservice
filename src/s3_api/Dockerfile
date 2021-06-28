FROM golang:1.16.5-alpine as build
WORKDIR /src

COPY go.* .
RUN go mod download

COPY *.go .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api .

FROM scratch as bin
COPY --from=build /out/api /
COPY app.env /

ENTRYPOINT [ "/api" ]
EXPOSE 80