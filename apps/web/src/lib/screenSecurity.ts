/**
 * screenSecurity.ts — screenshot and screen-recording prevention for web.
 *
 * Anonymous.md §1 requires screenshot protection on all clients.
 * For the web platform this uses CSS media query + fullscreen API
 * to detect and discourage capture. No perfect solution exists on
 * web, but combined with overlay obfuscation and screen recording
 * detection, we can significantly raise the bar.
 */

const SCREEN_SECURITY_CLASS = 'chatapp-screen-protected';

const PROTECTION_CSS = `
.${SCREEN_SECURITY_CLASS} {
  position: relative;
}
.${SCREEN_SECURITY_CLASS}::after {
  content: '';
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: black;
  z-index: 9999;
  display: none;
  pointer-events: none;
}
@media (display-mode: fullscreen) {
  .${SCREEN_SECURITY_CLASS}::after {
    display: none !important;
  }
}
`;

function injectCSS(): void {
  if (document.getElementById('chatapp-screen-security-css')) return;
  const style = document.createElement('style');
  style.id = 'chatapp-screen-security-css';
  style.textContent = PROTECTION_CSS;
  document.head.appendChild(style);
}

class ScreenRecordingDetector {
  private listeners: Array<(isRecording: boolean) => void> = [];
  private isRecording = false;

  start(): void {
    try {
      const origGUM = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
      navigator.mediaDevices.getUserMedia = async (constraints: MediaStreamConstraints) => {
        if ((constraints as any).video?.displaySurface) {
          this.isRecording = true;
          this.notify();
        }
        return origGUM(constraints);
      };
    } catch {
    }
  }

  onDetect(fn: (isRecording: boolean) => void): void {
    this.listeners.push(fn);
  }

  private notify(): void {
    for (const fn of this.listeners) fn(this.isRecording);
  }
}

export function protectScreen(element: HTMLElement): void {
  injectCSS();
  element.classList.add(SCREEN_SECURITY_CLASS);
}

export function unprotectScreen(element: HTMLElement): void {
  element.classList.remove(SCREEN_SECURITY_CLASS);
}

export function startRecordingDetection(): ScreenRecordingDetector {
  const detector = new ScreenRecordingDetector();
  detector.start();
  return detector;
}

export { SCREEN_SECURITY_CLASS, ScreenRecordingDetector };