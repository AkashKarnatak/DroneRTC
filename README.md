# DroneRTC
Run stream using
```sh
ffmpeg  -re -f v4l2 -i /dev/video0 -vcodec libvpx -b:v 2M -r 30  -cpu-used 2 -deadline 1 -g 10 -error-resilient 1 -auto-alt-ref 1 -f rtp 'rtp://127.0.0.1:5004?pkt_size=1200'
```
