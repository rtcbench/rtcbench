//go:build desktop

package app

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// lightsCenter is how far below the top of the window the traffic lights are centred, or 0 to leave them be
static CGFloat lightsCenter;

// layoutLights centres the traffic lights lightsCenter points below the top of the window, and as far in
// from its left edge as from its top, the way macOS's own title bars place them. AppKit lays the buttons
// out again when the window resizes or leaves full screen, so this runs after those too
static void layoutLights(NSWindow *window) {
	NSButton *close = [window standardWindowButton:NSWindowCloseButton];
	NSButton *minimize = [window standardWindowButton:NSWindowMiniaturizeButton];
	NSButton *zoom = [window standardWindowButton:NSWindowZoomButton];
	NSView *titlebar = close.superview.superview;
	if (lightsCenter <= 0 || titlebar == nil || (window.styleMask & NSWindowStyleMaskFullScreen)) return;
	// Frames snap to device pixels, so the buttons land the same way every time the band is a fraction of a point tall
	NSRect frame = titlebar.frame;
	frame.size.height = lightsCenter * 2;
	frame.origin.y = NSHeight(titlebar.superview.bounds) - frame.size.height;
	titlebar.frame = [titlebar.superview backingAlignedRect:frame options:NSAlignAllEdgesNearest];
	CGFloat spacing = NSMinX(minimize.frame) - NSMinX(close.frame);
	NSArray<NSButton *> *buttons = @[close, minimize, zoom];
	for (NSUInteger i = 0; i < buttons.count; i++) {
		NSButton *button = buttons[i];
		NSRect box = button.frame;
		box.origin.x = lightsCenter - NSHeight(box) / 2 + i * spacing;
		box.origin.y = (NSHeight(button.superview.bounds) - NSHeight(box)) / 2;
		button.frame = [button.superview backingAlignedRect:box options:NSAlignAllEdgesNearest];
	}
}

static void placeTrafficLights(double center) {
	dispatch_async(dispatch_get_main_queue(), ^{
		static NSWindow *watched;
		lightsCenter = center;
		for (NSWindow *window in NSApp.windows) {
			if ([window standardWindowButton:NSWindowCloseButton] == nil) continue;
			if (watched != window) {
				watched = window;
				void (^relayout)(NSNotification *) = ^(NSNotification *note) { layoutLights(window); };
				NSNotificationCenter *notifications = NSNotificationCenter.defaultCenter;
				[notifications addObserverForName:NSWindowDidResizeNotification object:window queue:nil usingBlock:relayout];
				[notifications addObserverForName:NSWindowDidExitFullScreenNotification object:window queue:nil usingBlock:relayout];
			}
			layoutLights(window);
			return;
		}
	});
}
*/
import "C"

import (
	"context"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func init() {
	// The page reports the centre of its title bar, in points from the top of the window, whenever it moves
	followTabs = func(window context.Context) {
		wailsruntime.EventsOn(window, "traffic-lights", func(data ...any) {
			if len(data) == 1 {
				if center, ok := data[0].(float64); ok && center > 0 {
					C.placeTrafficLights(C.double(center))
				}
			}
		})
	}
}
