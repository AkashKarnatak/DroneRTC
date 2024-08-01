# DroneRTC

DroneRTC is a peer-to-peer video communication project to send video feed and messages
from drone's camera to ground station using WebRTC

## Setup and Installation

### 1. Clone the repository:

```bash
git clone https://github.com/AkashKarnatak/DroneRTC.git
```

### 2. Navigate to the project directory:

```bash
cd DroneRTC
```

### 3. Running webrtc signaling server:

```bash
$ export SERVER_PORT=8080
$ node server/app.js
```

### 4. Running video client on drone:

```bash
$ cd drone/
$ go build
$ export HOST=ws://{server-ip}:{port}
$ # Use ffmpeg to convert video packets from drones camera to rtp
$ ffmpeg  -re -f v4l2 -i /dev/video0 -vcodec libvpx -b:v 2M -r 30  -cpu-used 2 -deadline 1 -g 10 -error-resilient 1 -auto-alt-ref 1 -f rtp 'rtp://127.0.0.1:5004?pkt_size=1200' &
$ # Run the drone client
$ ./drone
```

### 5. Running video client on ground station:

Create a file `receiver/env.js` with the following content,

```js
export const WEBSOCKET_URL = 'ws://{server-ip}:{server-port}'
```

then launch `index.html` file.

OR

just visit `http://{server-ip}:{SERVER_PORT}`, where `SERVER_PORT` is the port defined above.

## Contributing

Contributions are welcome! If you find a bug, have an idea for an enhancement, or want to contribute in any way, feel free to open an issue or submit a pull request.

## License

This project is licensed under the AGPL3 License. For details, see the [LICENSE](LICENSE) file.
