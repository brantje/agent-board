FROM node:24-alpine AS build

WORKDIR /src
RUN npm install --global pnpm@11.25.0

COPY package.json pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/package.json
RUN pnpm install --no-frozen-lockfile

COPY apps/web/ apps/web/
RUN pnpm --filter @agent-board/web build

FROM node:24-alpine

ENV NODE_ENV=production \
    NITRO_HOST=0.0.0.0 \
    NITRO_PORT=3000

WORKDIR /app
COPY --from=build --chown=node:node /src/apps/web/.output ./

USER node
EXPOSE 3000
CMD ["node", "server/index.mjs"]
