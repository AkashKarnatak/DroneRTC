FROM node:20.16.0-slim
WORKDIR /app
COPY ./package.json .
COPY ./server/ ./server
COPY ./receiver/ ./receiver

RUN npm i --omit=dev
RUN apt update && apt-get install -y python3-dev python3-opencv python3-wxgtk4.0 python3-pip python3-matplotlib python3-lxml python3-pygame
RUN pip3 install PyYAML mavproxy --break-system-packages
RUN echo 'mavproxy.py --master=udp:$HOST:14550 --daemon --default-modules link --streamrate -1 --out=udpin:$HOST:9000 &\nnode server/app.js' > entry.sh

ARG WEBSOCKET_URL

CMD bash entry.sh
