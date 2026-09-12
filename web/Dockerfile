FROM node:22-alpine AS ui-build
WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci
COPY app ./app
COPY react-router.config.js vite.config.js ./
RUN npm run build

FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production
ENV HOST=0.0.0.0
ENV PORT=3000

COPY --from=ui-build /app/package.json /app/package-lock.json ./
COPY --from=ui-build /app/node_modules ./node_modules
COPY --from=ui-build /app/build ./build

USER node
EXPOSE 3000
CMD ["npm", "run", "start"]
