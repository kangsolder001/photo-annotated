# syntax=docker/dockerfile:1
# Image frontend untuk Raspberry Pi (linux/arm64): build statis di arsitektur mesin build, lalu
# disajikan nginx non-root yang juga meneruskan /api ke container API.
#   docker buildx build --platform linux/arm64 -t photo-annotated-web:<tag> --load web/

FROM --platform=$BUILDPLATFORM node:24.21.0-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY . .
# kosong = API di origin yang sama (lewat proxy nginx di bawah)
ARG VITE_API_BASE_URL=
ARG VITE_AUTH_MODE=otp
ARG VITE_TURNSTILE_SITE_KEY=
RUN npm run build && rm -f dist/_headers dist/_redirects

FROM nginxinc/nginx-unprivileged:1.31.5-alpine
COPY nginx/default.conf /etc/nginx/conf.d/default.conf
COPY nginx/security-headers.conf /etc/nginx/snippets/security-headers.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
