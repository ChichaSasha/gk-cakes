// A short status message for the discovery pages (approved GK-016 r01c
// sketch: "Saved on this device for a year."), shown by ShopToast in the shop
// frame and announced politely. It clears itself after a few seconds. The
// text is cleared first and set a moment later, so the same message twice in
// a row is announced again.
class Toast {
	message = $state('');
	private timer: ReturnType<typeof setTimeout> | undefined;

	show(message: string): void {
		clearTimeout(this.timer);
		this.message = '';
		this.timer = setTimeout(() => {
			this.message = message;
			this.timer = setTimeout(() => (this.message = ''), 4000);
		}, 50);
	}
}

export const toast = new Toast();
