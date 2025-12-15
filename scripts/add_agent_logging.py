import re

with open('agent/cmd/sentinel-agent/main.go', 'r', encoding='utf-8') as f:
    content = f.read()

old_handler_start = '''func (a *Agent) handleWebRTCStart(msg *client.Message) error {
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

	log.Printf("WebRTC start request: sessionId=%s, quality=%s, hasOffer=%v", sessionID, quality, offerSdp != "")'''

new_handler_start = '''func (a *Agent) handleWebRTCStart(msg *client.Message) error {
	log.Printf("[WebRTC] handleWebRTCStart called, RequestID=%s", msg.RequestID)
	log.Printf("[WebRTC] msg.Data type: %T", msg.Data)

	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		log.Printf("[WebRTC] ERROR: Invalid message data type")
		return a.client.SendResponse(msg.RequestID, false, nil, "Invalid message data")
	}

	log.Printf("[WebRTC] data keys: %v", func() []string {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		return keys
	}())

	sessionID, _ := data["sessionId"].(string)
	offerSdp, _ := data["offerSdp"].(string)
	quality, _ := data["quality"].(string)
	if quality == "" {
		quality = "medium"
	}

	log.Printf("[WebRTC] Parsed: sessionId=%s, quality=%s, offerSdp length=%d", sessionID, quality, len(offerSdp))'''

if old_handler_start in content:
    content = content.replace(old_handler_start, new_handler_start)
    with open('agent/cmd/sentinel-agent/main.go', 'w', encoding='utf-8') as f:
        f.write(content)
    print('Added logging to agent handler')
else:
    print('Handler start not found')
    # Debug
    if 'handleWebRTCStart' in content:
        print('Found handleWebRTCStart')
    if 'sessionId=%s, quality=%s, hasOffer=%v' in content:
        print('Found log line')
