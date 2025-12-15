import re

with open('src/main/agents.ts', 'r', encoding='utf-8') as f:
    content = f.read()

old_code = """    // Store the session
    this.webrtcSessions.set(deviceId, {
      deviceId,
      agentId: device.agentId,
      quality: offer.quality || 'medium',
      active: true,
    });

    // Send WebRTC start command to agent
    await this.sendRequest(device.agentId, {
      type: 'webrtc_start',
      offer: {
        type: offer.type,
        sdp: offer.sdp,
      },
      quality: offer.quality || 'medium',
    });

    console.log(`Started WebRTC session for device ${deviceId}`);"""

new_code = """    // Generate a session ID
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

if old_code in content:
    content = content.replace(old_code, new_code)
    with open('src/main/agents.ts', 'w', encoding='utf-8') as f:
        f.write(content)
    print('Updated agents.ts')
else:
    print('Old code not found - checking for partial match')
    # Try to find the section
    if "// Send WebRTC start command to agent" in content:
        print("Found the comment marker")
    if "webrtc_start" in content:
        print("Found webrtc_start in content")
