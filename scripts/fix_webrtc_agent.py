import re

with open('agent/internal/webrtc/webrtc.go', 'r', encoding='utf-8') as f:
    content = f.read()

# Add CreateAnswer method after SetRemoteDescription
old_setremote = '''// SetRemoteDescription sets the remote SDP (answer from viewer)
func (s *Session) SetRemoteDescription(sdpType, sdp string) error {
	var sdpTypeEnum webrtc.SDPType
	switch sdpType {
	case "answer":
		sdpTypeEnum = webrtc.SDPTypeAnswer
	case "offer":
		sdpTypeEnum = webrtc.SDPTypeOffer
	default:
		sdpTypeEnum = webrtc.SDPTypeAnswer
	}

	err := s.PeerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: sdpTypeEnum,
		SDP:  sdp,
	})
	if err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}
	return nil
}'''

new_setremote = '''// SetRemoteDescription sets the remote SDP (offer or answer)
func (s *Session) SetRemoteDescription(sdpType, sdp string) error {
	var sdpTypeEnum webrtc.SDPType
	switch sdpType {
	case "answer":
		sdpTypeEnum = webrtc.SDPTypeAnswer
	case "offer":
		sdpTypeEnum = webrtc.SDPTypeOffer
	default:
		sdpTypeEnum = webrtc.SDPTypeAnswer
	}

	err := s.PeerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: sdpTypeEnum,
		SDP:  sdp,
	})
	if err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}
	return nil
}

// CreateAnswer creates an SDP answer after setting remote offer
func (s *Session) CreateAnswer() (string, error) {
	answer, err := s.PeerConnection.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create answer: %w", err)
	}

	err = s.PeerConnection.SetLocalDescription(answer)
	if err != nil {
		return "", fmt.Errorf("failed to set local description: %w", err)
	}

	// Wait for ICE gathering to complete
	gatherComplete := webrtc.GatheringCompletePromise(s.PeerConnection)
	select {
	case <-gatherComplete:
	case <-time.After(10 * time.Second):
		log.Printf("ICE gathering timed out, continuing with available candidates")
	}

	localDesc := s.PeerConnection.LocalDescription()
	if localDesc != nil {
		return localDesc.SDP, nil
	}
	return answer.SDP, nil
}'''

if old_setremote in content:
    content = content.replace(old_setremote, new_setremote)
    with open('agent/internal/webrtc/webrtc.go', 'w', encoding='utf-8') as f:
        f.write(content)
    print('Added CreateAnswer method to webrtc.go')
else:
    print('Could not find SetRemoteDescription method')
