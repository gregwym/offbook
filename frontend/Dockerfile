# syntax=docker/dockerfile:1.7
FROM node:22-alpine AS build
WORKDIR /src
# Pin pnpm (#585): unpinned corepack resolves the newest pnpm, whose native
# binary download can fail the build. Matches CI's pnpm 9 / lockfile v9.
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM nginx:alpine
COPY --from=build /src/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
