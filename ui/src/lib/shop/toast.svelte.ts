// A short status message for the discovery pages (approved GK-016 r01c
// sketch: "Saved on this device for a year."), shown by ShopToast in the shop
// frame and announced politely. It clears itself after a few seconds.
class Toast {
	message = $state('');
	private timer: ReturnType<typeof setTimeout> | undefined;

	show(message: string): void {
		clearTimeout(this.timer);
		this.message = message;
		this.timer = setTimeout(() => (this.message = ''), 4000);
	}
}

export const toast = new Toast();
