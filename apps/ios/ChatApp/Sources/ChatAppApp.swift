import SwiftUI

@main
struct ChatAppApp: App {
    @StateObject private var session = SessionStore()
    // Persisted light/dark choice, same default (dark) as every other client.
    @AppStorage("chatapp.theme") private var theme: String = "dark"

    var body: some Scene {
        WindowGroup {
            Group {
                if session.accessToken != nil {
                    MainTabView()
                        .environmentObject(session)
                } else {
                    LoginView()
                        .environmentObject(session)
                }
            }
            .preferredColorScheme(theme == "light" ? .light : .dark)
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
    @AppStorage("chatapp.theme") private var theme: String = "dark"

    var body: some View {
        NavigationStack {
            List {
                NavigationLink("Wallet", destination: WalletView())
                NavigationLink("Staking", destination: StakingView())
                NavigationLink("Monetization", destination: MonetizeView())
                NavigationLink("Bots", destination: BotsView())
                NavigationLink("Privacy", destination: PrivacyView())
                Section("Appearance") {
                    Toggle(isOn: Binding(
                        get: { theme == "dark" },
                        set: { theme = $0 ? "dark" : "light" }
                    )) {
                        Label("Dark theme", systemImage: theme == "dark" ? "moon.fill" : "sun.max.fill")
                    }
                }
            }
            .navigationTitle("More")
        }
    }
}
