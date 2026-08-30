import SwiftUI

struct FeedPost: Identifiable {
    let id: String
    let authorId: String
    let author: String
    let body: String
    var likeCount: Int
    var commentCount: Int
    var likedByMe: Bool
    var myReaction: String
    let feeling: String
    let location: String
    let edited: Bool
}

private let reactionEmoji: [(String, String)] = [
    ("like", "👍"), ("love", "❤️"), ("haha", "😂"),
    ("wow", "😮"), ("sad", "😢"), ("angry", "😡"),
]

struct FeedView: View {
    @EnvironmentObject var session: SessionStore
    @State private var posts: [FeedPost] = []
    @State private var draft = ""
    @State private var feeling = ""
    @State private var location = ""
    @State private var error: String?

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                HStack {
                    TextField("What's happening? #hashtags work", text: $draft)
                        .textFieldStyle(.roundedBorder)
                    Button("Post") { createPost() }
                        .buttonStyle(.borderedProminent)
                        .disabled(draft.trimmingCharacters(in: .whitespaces).isEmpty)
                }
                .padding()
                HStack {
                    TextField("Feeling/activity", text: $feeling)
                        .textFieldStyle(.roundedBorder)
                    TextField("Location", text: $location)
                        .textFieldStyle(.roundedBorder)
                }
                .padding(.horizontal)
                if let error {
                    Text(error).foregroundStyle(.red).font(.footnote)
                }
                List(posts) { post in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(post.author).font(.headline)
                        if !post.feeling.isEmpty || !post.location.isEmpty {
                            Text([
                                post.feeling.isEmpty ? nil : "is \(post.feeling)",
                                post.location.isEmpty ? nil : "at \(post.location)",
                            ].compactMap { $0 }.joined(separator: " "))
                            .font(.footnote).foregroundStyle(.secondary)
                        }
                        Text(post.body)
                        HStack(spacing: 10) {
                            ForEach(reactionEmoji, id: \.0) { kind, emoji in
                                Button {
                                    react(post, kind)
                                } label: {
                                    Text(post.myReaction == kind ? "\(emoji)✓" : emoji)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        HStack(spacing: 20) {
                            Label("\(post.likeCount)", systemImage: post.likedByMe ? "heart.fill" : "heart")
                            Label("\(post.commentCount)", systemImage: "bubble.right")
                            if post.edited {
                                Text("edited").font(.footnote)
                            }
                            if post.authorId == session.userId {
                                Button { pinPost(post) } label: {
                                    Label("Pin", systemImage: "pin")
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .foregroundStyle(.secondary)
                        .font(.subheadline)
                    }
                    .padding(.vertical, 4)
                }
            }
            .navigationTitle("Feed")
            .task { await load() }
            .refreshable { await load() }
        }
    }

    private func load() async {
        guard let token = session.accessToken else { return }
        do {
            let data = try await APIClient(token: token).get("/api/feed")
            guard let root = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let arr = root["posts"] as? [[String: Any]] else { return }
            posts = arr.map { p in
                FeedPost(
                    id: p["id"] as? String ?? "",
                    authorId: p["author_id"] as? String ?? "",
                    author: p["author_name"] as? String ?? "",
                    body: p["body"] as? String ?? "",
                    likeCount: p["like_count"] as? Int ?? 0,
                    commentCount: p["comment_count"] as? Int ?? 0,
                    likedByMe: p["liked_by_me"] as? Bool ?? false,
                    myReaction: p["my_reaction"] as? String ?? "",
                    feeling: p["feeling"] as? String ?? "",
                    location: p["location"] as? String ?? "",
                    edited: !(p["edited_at"] is NSNull) && (p["edited_at"] as? String ?? "").isEmpty == false
                )
            }
            error = nil
        } catch {
            self.error = "Failed to load feed"
        }
    }

    private func createPost() {
        guard let token = session.accessToken else { return }
        let body = draft
        let feel = feeling.trimmingCharacters(in: .whitespaces)
        let loc = location.trimmingCharacters(in: .whitespaces)
        Task {
            var payload: [String: Any] = ["type": "post", "body": body]
            if !feel.isEmpty { payload["feeling"] = feel }
            if !loc.isEmpty { payload["location"] = loc }
            _ = try? await APIClient(token: token).post("/api/posts", body: payload)
            draft = ""
            feeling = ""
            location = ""
            await load()
        }
    }

    private func react(_ post: FeedPost, _ kind: String) {
        guard let token = session.accessToken else { return }
        Task {
            if post.myReaction == kind {
                _ = try? await APIClient(token: token).delete("/api/posts/\(post.id)/react")
            } else {
                _ = try? await APIClient(token: token).put("/api/posts/\(post.id)/react",
                                                           body: ["reaction": kind])
            }
            await load()
        }
    }

    private func pinPost(_ post: FeedPost) {
        guard let token = session.accessToken else { return }
        Task {
            _ = try? await APIClient(token: token).put("/api/me/pinned-post",
                                                       body: ["post_id": post.id])
        }
    }
}
