import re

with open('src/main/agents.ts', 'r', encoding='utf-8') as f:
    content = f.read()

old_code = """    // Generate a session ID
    const sessionId = uuidv4();

    // Store the session
    this.webrtcSessions.set(deviceId, {
      deviceId,
      agentId: device.agentId,
      quality: offer.quality || 'medium',
      active: true,
    });

    // Send WebRTC start command to agent with offer SDP
    const result = await this.sendRequest(device.agentId, {
      type: 'webrtc_start',
      sessionId,
      offerSdp: offer.sdp,
      quality: offer.quality || 'medium',
    });

    // Forward the answer SDP back to the renderer
    if (result && result.answerSdp) {
      this.notifyRenderer('webrtc:signal', {
        deviceId,
        type: 'answer',
        sdp: result.answerSdp,
      });
    }

    console.log(`Started WebRTC session ${sessionId} for device ${deviceId}`);"""

new_code = """    // Generate a session ID
    const sessionId = uuidv4();
    console.log(`[WebRTC] Starting session ${sessionId} for device ${deviceId}`);
    console.log(`[WebRTC] Agent ID: ${device.agentId}, Quality: ${offer.quality || 'medium'}`);
    console.log(`[WebRTC] Offer SDP length: ${offer.sdp?.length || 0}`);

    // Store the session
    this.webrtcSessions.set(deviceId, {
      deviceId,
      agentId: device.agentId,
      quality: offer.quality || 'medium',
      active: true,
    });

    // Send WebRTC start command to agent with offer SDP
    console.log(`[WebRTC] Sending webrtc_start to agent...`);
    const result = await this.sendRequest(device.agentId, {
      type: 'webrtc_start',
      sessionId,
      offerSdp: offer.sdp,
      quality: offer.quality || 'medium',
    });
    console.log(`[WebRTC] Got result from agent:`, result);

    // Forward the answer SDP back to the renderer
    if (result && result.answerSdp) {
      console.log(`[WebRTC] Forwarding answer SDP to renderer (length: ${result.answerSdp.length})`);
      this.notifyRenderer('webrtc:signal', {
        deviceId,
        type: 'answer',
        sdp: result.answerSdp,
      });
    } else {
      console.log(`[WebRTC] WARNING: No answerSdp in result`);
    }

    console.log(`[WebRTC] Session ${sessionId} started successfully`);"""

if old_code in content:
    content = content.replace(old_code, new_code)
    with open('src/main/agents.ts', 'w', encoding='utf-8') as f:
        f.write(content)
    print('Added logging to agents.ts')
else:
    print('Code not found')
