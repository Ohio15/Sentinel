import re

with open('agent/cmd/sentinel-agent/main.go', 'r', encoding='utf-8') as f:
    content = f.read()

old_handler = '''func (a *Agent) handleWebRTCStart(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	sessionID, _ := data["sessionId"].(string)
	quality, _ := data["quality"].(string)
	if quality == "" {
		quality = "medium"
	}

	// Parse ICE servers if provided
	var iceServers []webrtc.ICEServer
	if servers, ok := data["iceServers"].([]interface{}); ok {
		for _, s := range servers {
			if serverMap, ok := s.(map[string]interface{}); ok {
				server := webrtc.ICEServer{}
				if urls, ok := serverMap["urls"].([]interface{}); ok {
					for _, u := range urls {
						if urlStr, ok := u.(string); ok {
							server.URLs = append(server.URLs, urlStr)
						}
					}
				}
				if username, ok := serverMap["username"].(string); ok {
					server.Username = username
				}
				if credential, ok := serverMap["credential"].(string); ok {
					server.Credential = credential
				}
				iceServers = append(iceServers, server)
			}
		}
	}

	config := webrtc.SessionConfig{
		SessionID:  sessionID,
		ICEServers: iceServers,
		Quality:    quality,
	}

	// Callback for signaling messages
	onSignal := func(signal webrtc.SignalMessage) {
		a.client.SendWebRTCSignal(signal.SessionID, signal.Type, signal.SDP, signal.Candidate)
	}

	// Callback for input events (mouse/keyboard from viewer)
	onInput := func(input webrtc.InputEvent) {
		// Reuse existing remote input handling
		if session, ok := a.remoteManager.GetSession(sessionID); ok {
			session.HandleInput(input.Type, map[string]interface{}{
				"type":      input.Type,
				"event":     input.Event,
				"x":         input.X,
				"y":         input.Y,
				"button":    input.Button,
				"key":       input.Key,
				"modifiers": input.Modifiers,
				"deltaY":    input.DeltaY,
			})
		}
	}

	session, err := a.webrtcManager.CreateSession(config, onSignal, onInput)
	if err != nil {
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	// Create SDP offer
	offer, err := session.CreateOffer()
	if err != nil {
		a.webrtcManager.StopSession(sessionID)
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"sessionId": sessionID,
		"offer":     offer,
	}, "")
}'''

new_handler = '''func (a *Agent) handleWebRTCStart(msg *client.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	sessionID, _ := data["sessionId"].(string)
	offerSdp, _ := data["offerSdp"].(string)
	quality, _ := data["quality"].(string)
	if quality == "" {
		quality = "medium"
	}

	log.Printf("WebRTC start request: sessionId=%s, quality=%s, hasOffer=%v", sessionID, quality, offerSdp != "")

	// Parse ICE servers if provided
	var iceServers []webrtc.ICEServer
	if servers, ok := data["iceServers"].([]interface{}); ok {
		for _, s := range servers {
			if serverMap, ok := s.(map[string]interface{}); ok {
				server := webrtc.ICEServer{}
				if urls, ok := serverMap["urls"].([]interface{}); ok {
					for _, u := range urls {
						if urlStr, ok := u.(string); ok {
							server.URLs = append(server.URLs, urlStr)
						}
					}
				}
				if username, ok := serverMap["username"].(string); ok {
					server.Username = username
				}
				if credential, ok := serverMap["credential"].(string); ok {
					server.Credential = credential
				}
				iceServers = append(iceServers, server)
			}
		}
	}

	config := webrtc.SessionConfig{
		SessionID:  sessionID,
		ICEServers: iceServers,
		Quality:    quality,
	}

	// Callback for signaling messages (ICE candidates)
	onSignal := func(signal webrtc.SignalMessage) {
		a.client.SendWebRTCSignal(signal.SessionID, signal.Type, signal.SDP, signal.Candidate)
	}

	// Callback for input events (mouse/keyboard from viewer)
	onInput := func(input webrtc.InputEvent) {
		// Handle input directly via webrtc session's input handler
		log.Printf("WebRTC input event: type=%s, event=%s", input.Type, input.Event)
	}

	session, err := a.webrtcManager.CreateSession(config, onSignal, onInput)
	if err != nil {
		log.Printf("Failed to create WebRTC session: %v", err)
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	// Set the remote offer from the browser
	if offerSdp != "" {
		log.Printf("Setting remote description (offer)")
		if err := session.SetRemoteDescription("offer", offerSdp); err != nil {
			log.Printf("Failed to set remote offer: %v", err)
			a.webrtcManager.StopSession(sessionID)
			return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
		}

		// Create answer
		log.Printf("Creating SDP answer")
		answerSdp, err := session.CreateAnswer()
		if err != nil {
			log.Printf("Failed to create answer: %v", err)
			a.webrtcManager.StopSession(sessionID)
			return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
		}

		log.Printf("WebRTC session started successfully, returning answer")
		return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
			"sessionId": sessionID,
			"answerSdp": answerSdp,
		}, "")
	}

	// Fallback: create offer if no remote offer provided (legacy mode)
	log.Printf("No offer provided, creating offer (legacy mode)")
	offer, err := session.CreateOffer()
	if err != nil {
		a.webrtcManager.StopSession(sessionID)
		return a.client.SendResponse(msg.RequestID, false, nil, err.Error())
	}

	return a.client.SendResponse(msg.RequestID, true, map[string]interface{}{
		"sessionId": sessionID,
		"offer":     offer,
	}, "")
}'''

if old_handler in content:
    content = content.replace(old_handler, new_handler)
    with open('agent/cmd/sentinel-agent/main.go', 'w', encoding='utf-8') as f:
        f.write(content)
    print('Updated handleWebRTCStart in main.go')
else:
    print('Could not find handleWebRTCStart')
    # Debug: check if partial match
    if 'handleWebRTCStart' in content:
        print('Found handleWebRTCStart function name')
    if 'Create SDP offer' in content:
        print('Found "Create SDP offer" comment')
