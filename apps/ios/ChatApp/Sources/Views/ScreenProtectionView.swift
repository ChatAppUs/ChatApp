import SwiftUI
import UIKit

public final class ScreenProtectionManager {
    public static let shared = ScreenProtectionManager()
    private var isProtected = false
    private weak var protectedField: UITextField?

    public func enable(on window: UIWindow) {
        guard !isProtected else { return }
        isProtected = true
        let field = UITextField()
        field.isSecureTextEntry = true
        field.isUserInteractionEnabled = false
        field.translatesAutoresizingMaskIntoConstraints = false
        window.addSubview(field)
        field.centerYAnchor.constraint(equalTo: window.centerYAnchor).isActive = true
        field.centerXAnchor.constraint(equalTo: window.centerXAnchor).isActive = true
        window.layer.superlayer?.addSublayer(field.layer)
        field.layer.sublayers?.first?.addSublayer(window.layer)
        protectedField = field
        NotificationCenter.default.addObserver(
            self, selector: #selector(handleCapture),
            name: UIScreen.capturedDidChangeNotification, object: nil
        )
    }

    public func disable(on window: UIWindow) {
        guard isProtected else { return }
        isProtected = false
        protectedField?.removeFromSuperview()
        protectedField = nil
        NotificationCenter.default.removeObserver(self)
    }

    @objc private func handleCapture() {
        if UIScreen.main.isCaptured {
            NotificationCenter.default.post(name: .screenRecordingDetected, object: nil)
        }
    }
}

public extension Notification.Name {
    static let screenRecordingDetected = Notification.Name("chatapp.screenRecordingDetected")
}

public struct ScreenProtected: ViewModifier {
    @State private var isRecording = UIScreen.main.isCaptured
    public func body(content: Content) -> some View {
        content
            .overlay(Group { if isRecording { Color.black.ignoresSafeArea() } })
            .onReceive(NotificationCenter.default.publisher(for: UIScreen.capturedDidChangeNotification)) { _ in
                isRecording = UIScreen.main.isCaptured
            }
    }
}

public extension View {
    func screenProtected() -> some View {
        modifier(ScreenProtected())
    }
}