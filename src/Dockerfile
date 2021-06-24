FROM golang:1.16.5-alpine as build
WORKDIR /src
COPY . .
RUN go build -o /out/api .

FROM scratch as bin
COPY --from=build /out/api /