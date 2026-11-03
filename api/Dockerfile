# API artifact: gk-api (default), gk-worker and gk-migrate in one image.
# Built once from a verified main commit and deployed by digest.
FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/gk-api ./cmd/gk-worker ./cmd/gk-migrate

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
LABEL org.opencontainers.image.source="https://github.com/ChichaSasha/gk-cakes" \
      org.opencontainers.image.description="GK Cakes API, worker and migration commands" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /out/ /usr/local/bin/
# Public Supabase Root 2021 CA for sslmode=verify-full
# (DATABASE_URL ...&sslrootcert=/etc/gk/supabase-root-2021.crt).
COPY certs/supabase-root-2021.crt /etc/gk/supabase-root-2021.crt
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gk-api"]
