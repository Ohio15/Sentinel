//go:build (linux && arm64) || (linux && arm)

package helper

import "errors"

// Headless stub for ARM Linux (Synology NAS, etc.)
// These systems don't have X11 displays, so desktop features are disabled

var ErrHeadlessMode = errors.New("desktop features not available in headless mode")

// InputInjector stub for headless systems
type InputInjector struct{}

func NewInputInjector() (*InputInjector, error) {
	return &InputInjector{}, nil
}

func (i *InputInjector) Close() error {
	return nil
}

func (i *InputInjector) MoveMouse(x, y int) error {
	return ErrHeadlessMode
}

func (i *InputInjector) MouseDown(button int) error {
	return ErrHeadlessMode
}

func (i *InputInjector) MouseUp(button int) error {
	return ErrHeadlessMode
}

func (i *InputInjector) Click(button int) error {
	return ErrHeadlessMode
}

func (i *InputInjector) DoubleClick(button int) error {
	return ErrHeadlessMode
}

func (i *InputInjector) Scroll(dx, dy int) error {
	return ErrHeadlessMode
}

func (i *InputInjector) KeyDown(keyCode uint16) error {
	return ErrHeadlessMode
}

func (i *InputInjector) KeyUp(keyCode uint16) error {
	return ErrHeadlessMode
}

func (i *InputInjector) KeyPress(keyCode uint16) error {
	return ErrHeadlessMode
}

func (i *InputInjector) TypeText(text string) error {
	return ErrHeadlessMode
}
