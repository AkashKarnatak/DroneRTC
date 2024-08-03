FROM node:20.16.0-slim
WORKDIR /app
COPY ./package.json .
COPY ./server/ ./server
COPY ./receiver/ ./receiver

RUN npm i --omit=dev

ARG WEBSOCKET_URL

RUN echo export const WEBSOCKET_URL=\"$WEBSOCKET_URL\" > receiver/env.js

CMD node server/app.js
