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
            FypView()
                .tabItem { Label("For You", systemImage: "play.rectangle") }
            ChatListView()
                .tabItem { Label("Chat", systemImage: "bubble.left.and.bubble.right") }
            GroupsView()
                .tabItem { Label("Groups", systemImage: "person.3") }
            PagesView()
                .tabItem { Label("Pages", systemImage: "flag") }
            MoreView()
                .tabItem { Label("More", systemImage: "ellipsis") }
        }
    }
}

struct MoreView: View {
    var body: some View {
        NavigationStack {
            List {
                NavigationLink("Monetization", destination: MonetizeView())
                NavigationLink("Bots", destination: BotsView())
                NavigationLink("Privacy", destination: PrivacyView())
            }
            .navigationTitle("More")
        }
    }
}
