# syntax=docker/dockerfile:1
FROM node:22.23.4-bookworm-slim AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./frontend/
RUN npm --prefix frontend ci --ignore-scripts
COPY frontend ./frontend
COPY ui ./ui
COPY design-system ./design-system
COPY scripts/release ./scripts/release
COPY LICENSE ./LICENSE
RUN npm --prefix frontend run build && node scripts/release/notices.mjs --npm-only

FROM golang:1.27.3-bookworm AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download && go mod verify
COPY backend ./
RUN mkdir -p /out/notices/go && find /go/pkg/mod -type f \( -iname "LICENSE*" -o -iname "NOTICE*" -o -iname "COPYING*" \) -exec cp --parents {} /out/notices/go \; && cp /usr/local/go/LICENSE /out/notices/Go-LICENSE.txt
RUN CGO_ENABLED=0 go build -trimpath -o /out/blog ./cmd/blog && CGO_ENABLED=0 go build -trimpath -o /out/publisher ./cmd/publisher

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=backend /out/blog /app/blog
COPY --from=backend /out/publisher /app/publisher
COPY --from=frontend /src/frontend/dist /app/frontend
COPY --from=frontend /src/build/notices /app/notices
COPY --from=backend /out/notices /app/notices
ENV TMPDIR=/var/lib/blog/tmp BLOG_ENV=production BLOG_DATA_ROOT=/var/lib/blog BLOG_FRONTEND_DIST=/app/frontend BLOG_LISTEN_ADDR=0.0.0.0:8080
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/blog"]
