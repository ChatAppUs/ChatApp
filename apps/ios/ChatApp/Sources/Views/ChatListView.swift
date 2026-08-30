import SwiftUI

struct Conversation: Identifiable {
    let id: String
    let title: String
    let isGroup: Bool
    let isChannel: Bool
    let lastMessage: String?
    let unread: Int
    let theme: String
}

private let chatThemeColors: [String: [Color]] = [
    "": [Color(.systemBackground), Color(.systemBackground)],
    "sunset": [Color(red: 1.0, green: 0.60, blue: 0.55), Color(red: 1.0, green: 0.42, blue: 0.53)],
    "ocean": [Color(red: 0.13, green: 0.58, blue: 0.69), Color(red: 0.43, green: 0.84, blue: 0.93)],
    "forest": [Color(red: 0.07, green: 0.31, blue: 0.37), Color(red: 0.44, green: 0.70, blue: 0.51)],
    "candy": [Color(red: 0.83, green: 0.58, blue: 0.61), Color(red: 0.75, green: 0.90, blue: 0.73)],
]
private let chatThemeNames = ["", "sunset", "ocean", "forest", "candy"]

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
                    unread: c["unread"] as? Int ?? 0,
                    theme: c["theme"] as? String ?? ""
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
    @State private var theme = ""
    @State private var nickname = ""
    @State private var typing = false
    @State private var socket: ChatSocket?

    var body: some View {
        VStack {
            ScrollView {
                HStack(spacing: 6) {
                    ForEach(chatThemeNames, id: \.self) { name in
                        Button(name.isEmpty ? "default" : name) { setTheme(name) }
                            .font(.caption)
                            .buttonStyle(.bordered)
                            .tint(theme == name ? .accentColor : .gray)
                    }
                }
                .padding(.top, 4)
                HStack {
                    TextField("My nickname in this chat", text: $nickname)
                        .textFieldStyle(.roundedBorder)
                    Button("Set") { setNickname() }
                }
                .padding(.horizontal)
            }
            .frame(maxHeight: 96)
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
                .background(
                    LinearGradient(colors: chatThemeColors[theme] ?? chatThemeColors[""]!,
                                   startPoint: .top, endPoint: .bottom)
                )
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
        theme = conversation.theme
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

    private func setTheme(_ name: String) {
        guard let token = session.accessToken else { return }
        theme = name
        Task {
            _ = try? await APIClient(token: token)
                .put("/api/conversations/\(conversation.id)/theme", body: ["theme": name])
        }
    }

    private func setNickname() {
        guard let token = session.accessToken, let uid = session.userId else { return }
        let nick = nickname.trimmingCharacters(in: .whitespaces)
        Task {
            _ = try? await APIClient(token: token)
                .put("/api/conversations/\(conversation.id)/nicknames/\(uid)",
                     body: ["nickname": nick])
        }
    }
}
