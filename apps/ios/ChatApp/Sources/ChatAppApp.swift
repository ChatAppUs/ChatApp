import SwiftUI

@main
struct ChatAppApp: App {
    @StateObject private var session = SessionStore()

    var body: some Scene {
        WindowGroup {
            if session.accessToken != nil {
                MainTabView()
                    .environmentObject(session)
            } else {
                LoginView()
                    .environmentObject(session)
            }
        }
    }
}

struct MainTabView: View {
    var body: some View {
        TabView {
            FeedView()
                .tabItem { Label("Feed", systemImage: "house") }
            ChatListView()
                .tabItem { Label("Chat", systemImage: "bubble.left.and.bubble.right") }
        }
    }
}
