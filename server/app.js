import express from 'express'
import fs from 'node:fs/promises'
import { WebSocket, WebSocketServer } from 'ws'

const SERVER_PORT = process.env.SERVER_PORT
const WEBSOCKET_URL = process.env.WEBSOCKET_URL

if (!SERVER_PORT || !WEBSOCKET_URL) {
  throw new Error('Forgot to initialze some variables')
}

// create env file for receiver
await fs.writeFile(
  './receiver/env.js',
  `export const WEBSOCKET_URL="${WEBSOCKET_URL}"`,
)

Array.prototype.random = function () {
  return this[Math.floor(Math.random() * this.length)]
}

WebSocket.prototype.init = function () {
  this.channels = new Map()
  this.on('message', (message) => {
    try {
      const { channel, data } = JSON.parse(message.toString())
      this.propagate(channel, data)
    } catch (e) {
      console.error(e)
    }
  })
}

WebSocket.prototype.register = function (channel, callback) {
  this.channels.set(channel, callback)
}

WebSocket.prototype.propagate = function (channel, data) {
  const callback = this.channels.get(channel)
  if (callback) {
    callback(data)
  } else if (this.peer) {
    // redirect message to peer
    return this.peer.send(JSON.stringify({ channel, data }))
  }
}

const app = express()
const port = SERVER_PORT

app.use(express.static('./receiver', { extensions: ['html'] }))

const server = app.listen(port, '0.0.0.0', () => {
  console.log(`Listening on port ${port}`)
})

const wss = new WebSocketServer({ server })

app.get('/online', (_, res) => {
  res.send({ online: wss.clients.size })
})

wss.drones = new Map()
wss.receivers = new Map()
wss.on('connection', (ws, req) => {
  console.log('new connection')
  ws.send(
    JSON.stringify({
      channel: 'message',
      data: JSON.stringify({ what: true }),
    }),
  )

  ws.init()

  ws.register('clientsOnline', () => {
    ws.send(
      JSON.stringify({ channel: 'clientsOnline', data: '' + wss.clients.size }),
    )
  })

  ws.register('match', async (data) => {
    const { type, id } = JSON.parse(data)
    if (!type || !id) {
      console.log('Type or Id not found')
      return
    }

    if (!['drone', 'receiver'].includes(type)) return

    console.log(type)

    ws.selfMap = type === 'drone' ? wss.drones : wss.receivers
    ws.peerMap = type === 'drone' ? wss.receivers : wss.drones

    // find peer
    const peer = Array.from(ws.peerMap.keys()).random()

    if (!peer) {
      // push to queue
      console.log('No peers found')
      console.log(
        `Pushing ${req.socket.remoteAddress}:${req.socket.remotePort} to queue`,
      )
      ws.selfMap.set(ws, id)
      return
    }

    const peerId = ws.peerMap.get(peer) // TODO: user it somewhere
    console.log('peer available:')
    console.log(
      `matching ${req.socket.remoteAddress}:${req.socket.remotePort} now`,
    )

    // remove peer from queue
    ws.peerMap.delete(peer)

    // set peer
    ws.peer = peer
    peer.peer = ws

    const drone = type === 'drone' ? ws : peer
    drone.send(JSON.stringify({ channel: 'begin', data: '' }))
  })

  ws.register('disconnect', async () => {
    if (!ws.peer) return
    ws.peer.peer = undefined
    ws.peer.send(JSON.stringify({ channel: 'disconnect', data: '' }))
    ws.peer = undefined
  })

  ws.on('close', () => {
    console.log(
      `${req.socket.remoteAddress}:${req.socket.remotePort} disconnected`,
    )
    if (ws.peer) {
      ws.peer.send(JSON.stringify({ channel: 'disconnect', data: '' }))
      ws.peer.peer = undefined
    }
    // remove self from queue if present
    ws.selfMap?.delete(ws)
  })
})
