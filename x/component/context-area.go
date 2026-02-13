// SPDX-License-Identifier: Unlicense OR MIT

package component

import (
	"image"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// ContextArea is a region of the UI that responds to certain
// pointer events by displaying a contextual widget. The contextual
// widget is overlaid using an op.DeferOp. The contextual widget
// can be dismissed by primary-clicking within or outside of it.
type ContextArea struct {
	lastUpdate time.Time
	position   f32.Point
	// absolutePosition is the click position in window coordinates
	absolutePosition f32.Point
	// transform is the transformation matrix at event time
	transform     f32.Affine2D
	dims          D
	active        bool
	startedActive bool
	justActivated bool
	justDismissed bool
	// Activation is the pointer Buttons within the context area
	// that trigger the presentation of the contextual widget. If this
	// is zero, it will default to pointer.ButtonSecondary.
	Activation pointer.Buttons
	// AbsolutePosition will position the contextual widget in the
	// relative to the position of the context area instead of relative
	// to the position of the click event that triggered the activation.
	// This is useful for controls (like button-activated menus) where
	// the contextual content should not be precisely attached to the
	// click position, but should instead be attached to the button.
	AbsolutePosition bool
	// PositionHint tells the ContextArea the closest edge/corner of the
	// window to where it is being used in the layout. This helps it to
	// position the contextual widget without it overflowing the edge of
	// the window.
	PositionHint layout.Direction
}

// Update performs event processing for the context area but does not lay it out.
// It is automatically invoked by Layout() if it has not already been called during
// a given frame.
func (r *ContextArea) Update(gtx C) {
	if gtx.Now == r.lastUpdate {
		return
	}
	r.lastUpdate = gtx.Now
	if r.Activation == 0 {
		r.Activation = pointer.ButtonSecondary
	}
	suppressionTag := &r.active
	dismissTag := &r.dims

	r.startedActive = r.active
	// Summon the contextual widget if the area recieved a secondary click.
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: r,
			Kinds:  pointer.Press | pointer.Release,
		})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		if r.active {
			// Check whether we should dismiss menu.
			if e.Buttons.Contain(pointer.ButtonPrimary) {
				clickPos := e.Position.Sub(r.position)
				min := f32.Point{}
				max := f32.Point{
					X: float32(r.dims.Size.X),
					Y: float32(r.dims.Size.Y),
				}
				if !(clickPos.X > min.X && clickPos.Y > min.Y && clickPos.X < max.X && clickPos.Y < max.Y) {
					r.Dismiss()
				}
			}
		}
		if e.Buttons.Contain(r.Activation) && e.Kind == pointer.Press {
			r.active = true
			r.justActivated = true
			if !r.AbsolutePosition {
				r.position = e.Position
				// Store the absolute position and transform
				r.absolutePosition = e.AbsolutePosition()
				r.transform = e.Transform
			}
		}
	}

	// Dismiss the contextual widget if the user clicked outside of it.
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: suppressionTag,
			Kinds:  pointer.Press,
		})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		if e.Kind == pointer.Press {
			r.Dismiss()
		}
	}
	// Dismiss the contextual widget if the user released a click within it.
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: dismissTag,
			Kinds:  pointer.Release,
		})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		if e.Kind == pointer.Release {
			r.Dismiss()
		}
	}
}

// Layout renders the context area and -- if the area is activated by an
// appropriate gesture -- also the provided widget overlaid using an op.DeferOp.
func (r *ContextArea) Layout(gtx C, w layout.Widget) D {
	r.Update(gtx)
	suppressionTag := &r.active
	dismissTag := &r.dims
	dims := D{Size: gtx.Constraints.Min}

	var contextual op.CallOp
	if r.active || r.startedActive {
		// Render if the layout started as active to ensure that widgets
		// within the contextual content get to update their state in reponse
		// to the event that dismissed the contextual widget.
		contextual = func() op.CallOp {
			macro := op.Record(gtx.Ops)
			r.dims = w(gtx)
			return macro.Stop()
		}()
	}

	if r.active {
		// Use window coordinates for boundary checking
		windowSize := gtx.WindowSize
		menuWidth := r.dims.Size.X
		menuHeight := r.dims.Size.Y

		// Calculate menu position in window coordinates
		absPosX := int(math.Round(float64(r.absolutePosition.X)))
		absPosY := int(math.Round(float64(r.absolutePosition.Y)))

		// Adjust horizontal position if menu would overflow window right edge
		if absPosX+menuWidth > windowSize.X {
			absPosX = max(
				// If still negative, clamp to left edge
				absPosX-menuWidth, 0)
		}

		// Adjust vertical position if menu would overflow window bottom edge
		if absPosY+menuHeight > windowSize.Y {
			absPosY = max(
				// If still negative, clamp to top edge
				absPosY-menuHeight, 0)
		}

		// Use the saved transform to convert window coordinates back to local coordinates
		localPos := r.transform.Invert().Transform(f32.Pt(float32(absPosX), float32(absPosY)))

		// Lay out a transparent scrim to block input to things beneath the
		// contextual widget.
		suppressionScrim := func() op.CallOp {
			macro2 := op.Record(gtx.Ops)
			pr := clip.Rect(image.Rectangle{Min: image.Point{-1e6, -1e6}, Max: image.Point{1e6, 1e6}})
			stack := pr.Push(gtx.Ops)
			event.Op(gtx.Ops, suppressionTag)
			stack.Pop()
			return macro2.Stop()
		}()
		op.Defer(gtx.Ops, suppressionScrim)

		// Lay out the contextual widget itself.
		// Convert local position back to integer offset
		pos := image.Point{
			X: int(math.Round(float64(localPos.X))),
			Y: int(math.Round(float64(localPos.Y))),
		}
		macro := op.Record(gtx.Ops)
		op.Offset(pos).Add(gtx.Ops)
		contextual.Add(gtx.Ops)

		// Lay out a scrim on top of the contextual widget to detect
		// completed interactions with it (that should dismiss it).
		pt := pointer.PassOp{}.Push(gtx.Ops)
		stack := clip.Rect(image.Rectangle{Max: r.dims.Size}).Push(gtx.Ops)
		event.Op(gtx.Ops, dismissTag)

		stack.Pop()
		pt.Pop()
		contextual = macro.Stop()
		op.Defer(gtx.Ops, contextual)
	}

	// Capture pointer events in the contextual area.
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect(image.Rectangle{Max: gtx.Constraints.Min}).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, r)

	return dims
}

// Dismiss sets the ContextArea to not be active.
func (r *ContextArea) Dismiss() {
	r.active = false
	r.justDismissed = true
}

// Active returns whether the ContextArea is currently active (whether
// it is currently displaying overlaid content or not).
func (r ContextArea) Active() bool {
	return r.active
}

// Activated returns true if the context area has become active since
// the last call to Activated.
func (r *ContextArea) Activated() bool {
	defer func() {
		r.justActivated = false
	}()
	return r.justActivated
}

// Dismissed returns true if the context area has been dismissed since
// the last call to Dismissed.
func (r *ContextArea) Dismissed() bool {
	defer func() {
		r.justDismissed = false
	}()
	return r.justDismissed
}
