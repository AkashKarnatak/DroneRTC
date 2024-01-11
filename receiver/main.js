import { createSocket } from './socket.js'

const $ = (x) => document.querySelector(x)

const ws = await createSocket()

let pc

const $videoPeer = $('#video-peer')
const $messageBox = $('#message-box')
const $sendBtn = $('#send-btn')

$sendBtn.addEventListener('click', () => {
  ws.emit('msg', $messageBox.value)
  $messageBox.value = ''
})

const initializeConnection = async () => {
  console.log('creating rtc')

  const iceConfig = {
    iceServers: [
      {
        urls: ['stun:stun.l.google.com:19302', 'stun:stun1.l.google.com:19302'],
      },
    ],
  }

  pc = new RTCPeerConnection(iceConfig)
  pc.addTransceiver('video', { direction: 'recvonly' })

  pc.oniceconnectionstatechange = async function () {
    console.log(pc.iceConnectionState)
    if (
      pc.iceConnectionState === 'failed' ||
      pc.iceConnectionState === 'disconnected' ||
      pc.iceConnectionState === 'closed'
    ) {
      pc.close()
      await initializeConnection()
    }
  }

  pc.onicecandidate = (e) => {
    if (!e.candidate) return
    const ice = JSON.stringify(e.candidate)
    console.log('new ice candidate', ice)
    ws.emit('iceCandidate', ice)
  }


  pc.ontrack = (event) => {
    console.log('received track')
    $videoPeer.srcObject = event.streams[0]
  }

  ws.emit('clientsOnline')
  ws.emit('match', JSON.stringify({ type: 'receiver', id: 'droneId' })) // TODO: create random id
}

ws.register('clientsOnline', async (data) => {
  console.log(data)
})

ws.register('connected', async (data) => {
  console.log('connected')
})

ws.register('iceCandidate', async (data) => {
  const ice = JSON.parse(data)
  await pc.addIceCandidate(ice)
})

ws.register('description', async (data) => {
  console.log('received description')
  const desc = JSON.parse(data)
  await pc.setRemoteDescription(desc)
  const answer = await pc.createAnswer()
  ws.emit('description', JSON.stringify(answer))
  await pc.setLocalDescription(answer)
})

ws.register('disconnect', async () => {
  console.log('received disconnect request')
  pc.close()
  await initializeConnection()
})

await initializeConnection()
