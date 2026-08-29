import SwiftUI

struct Conversation: Identifiable {
    let id: String
    let title: String
    let isGroup: Bool
    let isChannel: Bool
    let lastMessage: String?
    let unread: Int
}

struct ChatListView: View {
    @EnvironmentObject var session: SessionStore
    @State private var conversations: [Conversation] = []

    var body: some View {
        NavigationStack {
            List(conversations) { conv in
                NavigationLink(destination: ChatView(conversation: conv)) {
                    VStack(alignment: .leading) {
                        HStack {
                            Text(conv.title.isEmpty
                                 ? (conv.isChannel ? "📢 Channel" : conv.isGroup ? "👥 Group" : "DM")
                                 : conv.title)
                                .font(.headline)
                            Spacer()
                            if conv.unread > 0 {
                                Text("\(conv.unread)")
                                    .font(.caption2)
                                    .padding(6)
                                    .background(Color.accentColor, in: Circle())
                                    .foregroundStyle(.white)
                            }
                        }
                        if let last = conv.lastMessage {
                            Text(last).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                }
            }
            .navigationTitle("Chats")
            .task { await load() }
            .refreshable { await load() }
        }
    }

    private func load() async {
        guard let token = session.accessToken else { return }
        do {
            let data = try await APIClient(token: token).get("/api/conversations")
            guard let root = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let arr = root["conversations"] as? [[String: Any]] else { return }
            conversations = arr.map { c in
                Conversation(
                    id: c["id"] as? String ?? "",
                    title: c["title"] as? String ?? "",
                    isGroup: c["is_group"] as? Bool ?? false,
                    isChannel: c["is_channel"] as? Bool ?? false,
                    lastMessage: c["last_message"] as? String,
                    unread: c["unread"] as? Int ?? 0
                )
            }
        } catch {
            // keep stale list
        }
    }
}

struct ChatMessage: Identifiable {
    let id: String
    let senderId: String
    let body: String
    let encrypted: Bool
}

struct ChatView: View {
    @EnvironmentObject var session: SessionStore
    let conversation: Conversation

    @State private var messages: [ChatMessage] = []
    @State private var draft = ""
    @State private var typing = false
    @State private var socket: ChatSocket?

    var body: some View {
        VStack {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 6) {
                        ForEach(messages) { m in
                            HStack {
                                if m.senderId == session.userId { Spacer() }
                                Text((m.encrypted ? "🔒 " : "") + m.body)
                                    .padding(10)
                                    .background(m.senderId == session.userId
                                                ? Color.accentColor.opacity(0.3)
                                                : Color.gray.opacity(0.2),
                                                in: RoundedRectangle(cornerRadius: 12))
                                if m.senderId != session.userId { Spacer() }
                            }
                            .id(m.id)
                        }
                    }
                    .padding()
                }
                .onChange(of: messages.count) { _, _ in
                    if let last = messages.last {
                        withAnimation { proxy.scrollTo(last.id) }
                    }
                }
            }
            if typing {
                Text("typing…").font(.caption).foregroundStyle(.secondary)
            }
            HStack {
                TextField("Message", text: $draft)
                    .textFieldStyle(.roundedBorder)
                    .onChange(of: draft) { _, _ in
                        socket?.sendTyping(conversationId: conversation.id)
                    }
                Button("Send") {
                    socket?.sendMessage(conversationId: conversation.id, body: draft)
                    draft = ""
                }
                .buttonStyle(.borderedProminent)
                .disabled(draft.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            .padding()
        }
        .navigationTitle(conversation.title.isEmpty ? "Chat" : conversation.title)
        .onAppear { connect() }
        .onDisappear { socket?.close() }
    }

    private func connect() {
        guard let token = session.accessToken else { return }
        let s = ChatSocket()
        s.onEvent = { evt in
            guard (evt["type"] as? String) == "message",
                  (evt["conversation_id"] as? String) == conversation.id else {
                if (evt["type"] as? String) == "typing" { typing = true
                    DispatchQueue.main.asyncAfter(deadline: .now() + 3) { typing = false } }
                return
            }
            let msg = ChatMessage(
                id: evt["id"] as? String ?? UUID().uuidString,
                senderId: evt["sender_id"] as? String ?? "",
                body: evt["body"] as? String ?? "",
                encrypted: evt["is_encrypted"] as? Bool ?? false
            )
            DispatchQueue.main.async { messages.append(msg) }
        }
        s.connect(token: token)
        socket = s

        Task {
            if let data = try? await APIClient(token: token)
                .get("/api/conversations/\(conversation.id)/messages"),
               let root = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               let arr = root["messages"] as? [[String: Any]] {
                let history = arr.reversed().map { m in
                    ChatMessage(
                        id: m["id"] as? String ?? "",
                        senderId: m["sender_id"] as? String ?? "",
                        body: m["body"] as? String ?? "",
                        encrypted: m["is_encrypted"] as? Bool ?? false
                    )
                }
                await MainActor.run { messages = history }
                _ = try? await APIClient(token: token)
                    .post("/api/conversations/\(conversation.id)/read")
            }
        }
    }
}
