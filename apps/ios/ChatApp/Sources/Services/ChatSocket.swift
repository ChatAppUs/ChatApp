import Foundation

/// Realtime socket over URLSessionWebSocketTask: chat messages, typing,
/// receipts, reactions and WebRTC signaling.
final class ChatSocket: NSObject {
    var onEvent: ([String: Any]) -> Void = { _ in }
    var onClose: () -> Void = {}

    private var task: URLSessionWebSocketTask?
    private var session: URLSession?

    func connect(token: String) {
        let url = URL(string: "\(APIClient.wsBaseURL)/ws?token=\(token)")!
        session = URLSession(configuration: .default, delegate: self, delegateQueue: nil)
        task = session?.webSocketTask(with: url)
        task?.resume()
        receive()
    }

    private func receive() {
        task?.receive { [weak self] result in
            guard let self else { return }
            switch result {
            case .success(.string(let text)):
                if let data = text.data(using: .utf8),
                   let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                    self.onEvent(json)
                }
                self.receive()
            case .success:
                self.receive()
            case .failure:
                self.onClose()
            }
        }
    }

    func send(_ payload: [String: Any]) {
        guard let data = try? JSONSerialization.data(withJSONObject: payload),
              let text = String(data: data, encoding: .utf8) else { return }
        task?.send(.string(text)) { _ in }
    }

    func sendMessage(conversationId: String, body: String, encrypted: Bool = false) {
        send([
            "type": "message",
            "conversation_id": conversationId,
            "body": body,
            "is_encrypted": encrypted,
        ])
    }

    func sendTyping(conversationId: String) {
        send(["type": "typing", "conversation_id": conversationId])
    }

    func sendSignal(conversationId: String, signal: [String: Any]) {
        send(["type": "signal", "conversation_id": conversationId, "signal": signal])
    }

    func close() {
        task?.cancel(with: .normalClosure, reason: nil)
        session?.invalidateAndCancel()
    }
}

extension ChatSocket: URLSessionWebSocketDelegate {}
