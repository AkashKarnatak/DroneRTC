import { createSocket } from './socket.js'

const $ = (x) => document.querySelector(x)

const ws = await createSocket()

let pc

const $videoPeer = $('#video-peer')
const $messageBox = $('#message-box')
const $sendBtn = $('#send-btn')
const $loader = $('#peer-video-loader')
const $downloadBtn = $('#download-btn')

$sendBtn.addEventListener('click', () => {
  console.log('Sending msg:', $messageBox.value)
  ws.emit('msg', $messageBox.value)
  $messageBox.value = ''
})
$messageBox.focus()
$messageBox.addEventListener('keydown', function (e) {
  if (e.key === 'Enter') {
    $sendBtn.click()
    return e.preventDefault()
  }
})

// hide loader when video connected
$videoPeer.addEventListener('play', () => {
  $loader.style.display = 'none'
})

$downloadBtn.addEventListener('click', () => {
  stopRecording()
})

let recorder, stream
const chunks = []

const stopRecording = () => {
  try {
    if (recorder.state != 'inactive') {
      console.log('stopping')
      recorder.stop()
    }
  } catch (error) {
    console.log('recorder already inactive')
  }
}

const download = () => {
  const blob = new Blob(chunks, { type: 'video/webm' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.target = '_blank'
  a.click()
  console.log(url)
  chunks.length = 0
  recorder.start(1000)
}

const initializeConnection = async () => {
  console.log('creating rtc')

  $loader.style.display = 'inline-block' // show loader

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
    if (pc.iceConnectionState === 'failed') {
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

    stream = event.streams[0]
    chunks.length = 0

    recorder = new MediaRecorder(stream, {
      mimeType: 'video/webm',
    })

    recorder.ondataavailable = async (e) => {
      if (e.data.size > 0) {
        chunks.push(e.data)
      }
    }
    recorder.onerror = stopRecording
    recorder.onstop = download
    recorder.start(1000)
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
