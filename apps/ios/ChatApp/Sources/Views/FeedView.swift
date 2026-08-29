import SwiftUI

struct FeedPost: Identifiable {
    let id: String
    let author: String
    let body: String
    var likeCount: Int
    var commentCount: Int
    var likedByMe: Bool
}

struct FeedView: View {
    @EnvironmentObject var session: SessionStore
    @State private var posts: [FeedPost] = []
    @State private var draft = ""
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
                if let error {
                    Text(error).foregroundStyle(.red).font(.footnote)
                }
                List(posts) { post in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(post.author).font(.headline)
                        Text(post.body)
                        HStack(spacing: 20) {
                            Button {
                                toggleLike(post)
                            } label: {
                                Label("\(post.likeCount)", systemImage: post.likedByMe ? "heart.fill" : "heart")
                            }
                            .buttonStyle(.plain)
                            Label("\(post.commentCount)", systemImage: "bubble.right")
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
                    author: p["author_name"] as? String ?? "",
                    body: p["body"] as? String ?? "",
                    likeCount: p["like_count"] as? Int ?? 0,
                    commentCount: p["comment_count"] as? Int ?? 0,
                    likedByMe: p["liked_by_me"] as? Bool ?? false
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
        Task {
            _ = try? await APIClient(token: token).post("/api/posts", body: [
                "type": "post", "body": body,
            ])
            draft = ""
            await load()
        }
    }

    private func toggleLike(_ post: FeedPost) {
        guard let token = session.accessToken else { return }
        Task {
            if post.likedByMe {
                _ = try? await APIClient(token: token).delete("/api/posts/\(post.id)/like")
            } else {
                _ = try? await APIClient(token: token).post("/api/posts/\(post.id)/like")
            }
            await load()
        }
    }
}
