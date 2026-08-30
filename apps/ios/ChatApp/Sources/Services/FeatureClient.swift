import Foundation

// Typed client for the feature endpoints added in GAP_ANALYSIS. All calls go
// through APIClient with the session token; decodable models keep call sites
// safe and consistent with the shared API contract (same surface as web and
// Android).
struct FeatureClient {
    let api: APIClient

    init(token: String?) {
        api = APIClient(token: token)
    }

    struct Group: Decodable {
        let id: String
        let name: String
        let description: String?
        let privacy: String?
        let member_count: Int?
    }
    struct GroupList: Decodable { let groups: [Group] }
    struct Event: Decodable {
        let id: String
        let title: String
        let starts_at: String
        let location: String?
        let group_id: String
        let going_count: Int
    }
    struct EventList: Decodable { let events: [Event] }

    struct PageInfo: Decodable {
        let id: String
        let name: String
        let category: String
        let description: String?
        let follower_count: Int
    }
    struct PageList: Decodable { let pages: [PageInfo] }

    struct Tier: Decodable {
        let id: String
        let name: String
        let perks: String
        let price_usd: Double
        let subscriber_count: Int
    }
    struct TierList: Decodable { let tiers: [Tier] }

    struct Subscription: Decodable {
        let id: String
        let tier_name: String
        let price_usd: Double
        let creator_username: String
        let status: String
        let current_period_end: String
    }
    struct SubList: Decodable { let subscriptions: [Subscription] }

    struct Earnings: Decodable {
        let earned: Double
        let paid_out: Double
        let available: Double
        let currency: String
    }

    struct Bot: Decodable {
        let id: String
        let username: String
        let display_name: String
        let description: String?
        let active: Bool
        let has_webhook: Bool?
        let mini_app_url: String
    }
    struct BotList: Decodable { let bots: [Bot] }

    struct Person: Decodable { let id: String let username: String }
    struct MutesList: Decodable { let mutes: [Person] }
    struct RestrictedList: Decodable { let restricted: [Person] }
    struct Filter: Decodable { let phrase: String let created_at: String }
    struct FilterList: Decodable { let filters: [Filter] }
    struct FollowReq: Decodable { let id: String let username: String }
    struct FollowReqList: Decodable { let requests: [FollowReq] }
    struct MsgReq: Decodable {
        let conversation_id: String
        let username: String
        let preview: String?
    }
    struct MsgReqList: Decodable { let requests: [MsgReq] }

    static func decoded<T: Decodable>(_ type: T.Type, from data: Data) -> T? {
        try? JSONDecoder().decode(from: data)
    }

    func listGroups(query: String = "") async throws -> GroupList {
        let q = query.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? query
        return decoded(GroupList.self, from: try await api.get("/api/groups?q=\(q)&limit=50"))
            ?? GroupList(groups: [])
    }

    func joinGroup(_ id: String) async throws {
        _ = try await api.post("/api/groups/\(id)/join")
    }

    func leaveGroup(_ id: String) async throws {
        _ = try await api.delete("/api/groups/\(id)/join")
    }

    func createGroup(name: String, description: String, isPrivate: Bool) async throws {
        _ = try await api.post("/api/groups", body: [
            "name": name, "description": description,
            "privacy": isPrivate ? "private" : "public",
        ])
    }

    func listEvents() async throws -> EventList {
        decoded(EventList.self, from: try await api.get("/api/events?limit=50"))
            ?? EventList(events: [])
    }

    func rsvp(_ eventId: String, response: String) async throws {
        _ = try await api.post("/api/events/\(eventId)/rsvp", body: ["response": response])
    }

    func listPages(query: String = "") async throws -> PageList {
        let q = query.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? query
        return decoded(PageList.self, from: try await api.get("/api/pages?q=\(q)&limit=50"))
            ?? PageList(pages: [])
    }

    func createPage(name: String, category: String, description: String) async throws {
        _ = try await api.post("/api/pages", body: [
            "name": name, "category": category, "description": description,
        ])
    }

    func followPage(_ id: String) async throws {
        _ = try await api.post("/api/pages/\(id)/follow")
    }

    func unfollowPage(_ id: String) async throws {
        _ = try await api.delete("/api/pages/\(id)/follow")
    }

    func myTiers() async throws -> TierList {
        decoded(TierList.self, from: try await api.get("/api/creator/tiers"))
            ?? TierList(tiers: [])
    }

    func createTier(name: String, perks: String, priceUsd: Double) async throws {
        _ = try await api.post("/api/creator/tiers", body: [
            "name": name, "perks": perks, "price_usd": priceUsd,
        ])
    }

    func subscriptions() async throws -> SubList {
        decoded(SubList.self, from: try await api.get("/api/subscriptions"))
            ?? SubList(subscriptions: [])
    }

    func cancelSubscription(_ subscriptionId: String) async throws {
        _ = try await api.delete("/api/subscriptions/\(subscriptionId)")
    }

    func earnings() async throws -> Earnings? {
        decoded(Earnings.self, from: try await api.get("/api/creator/earnings"))
    }

    struct UserHit: Decodable { let id: String let username: String }
    private struct UserSearch: Decodable { let users: [UserHit] }

    private func findUser(_ username: String) async throws -> String {
        let q = username.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? username
        let data = try await api.get("/api/users/search?q=\(q)")
        guard let hit = decoded(UserSearch.self, from: data)?.users.first else {
            throw APIError.http(404, "user not found")
        }
        return hit.id
    }

    func tip(to: String, amountUsd: Double, message: String = "") async throws {
        let uid = try await findUser(to)
        _ = try await api.post("/api/users/\(uid)/tip", body: [
            "amount_usd": amountUsd, "message": message,
        ])
    }

    func myBots() async throws -> BotList {
        decoded(BotList.self, from: try await api.get("/api/bots"))
            ?? BotList(bots: [])
    }

    struct BotCreated: Decodable { let token: String }

    func createBot(username: String, displayName: String, description: String) async throws -> String? {
        let data = try await api.post("/api/bots", body: [
            "username": username, "display_name": displayName, "description": description,
        ])
        return decoded(BotCreated.self, from: data)?.token
    }

    func deleteBot(_ id: String) async throws {
        _ = try await api.delete("/api/bots/\(id)")
    }

    func mutes() async throws -> MutesList {
        decoded(MutesList.self, from: try await api.get("/api/me/mutes"))
            ?? MutesList(mutes: [])
    }

    func restricted() async throws -> RestrictedList {
        decoded(RestrictedList.self, from: try await api.get("/api/me/restricted"))
            ?? RestrictedList(restricted: [])
    }

    func filters() async throws -> FilterList {
        decoded(FilterList.self, from: try await api.get("/api/me/word-filters"))
            ?? FilterList(filters: [])
    }

    func addFilter(_ phrase: String) async throws {
        _ = try await api.post("/api/me/word-filters", body: ["phrase": phrase])
    }

    func removeFilter(_ phrase: String) async throws {
        _ = try await api.delete("/api/me/word-filters", body: ["phrase": phrase])
    }

    func followRequests() async throws -> FollowReqList {
        decoded(FollowReqList.self, from: try await api.get("/api/me/follow-requests"))
            ?? FollowReqList(requests: [])
    }

    func acceptFollowRequest(_ id: String) async throws {
        _ = try await api.post("/api/me/follow-requests/\(id)/accept")
    }

    func declineFollowRequest(_ id: String) async throws {
        _ = try await api.post("/api/me/follow-requests/\(id)/decline")
    }

    func messageRequests() async throws -> MsgReqList {
        decoded(MsgReqList.self, from: try await api.get("/api/me/message-requests"))
            ?? MsgReqList(requests: [])
    }

    func acceptMessageRequest(_ conversationId: String) async throws {
        _ = try await api.post("/api/me/message-requests/\(conversationId)/accept")
    }

    func declineMessageRequest(_ conversationId: String) async throws {
        _ = try await api.post("/api/me/message-requests/\(conversationId)/decline")
    }

    func unmute(_ id: String) async throws {
        _ = try await api.delete("/api/users/\(id)/mute")
    }

    func unrestrict(_ id: String) async throws {
        _ = try await api.delete("/api/users/\(id)/restrict")
    }

    func setProfileLock(_ locked: Bool) async throws {
        _ = try await api.put("/api/me/profile-lock", body: ["locked": locked])
    }

    func setActiveStatus(_ show: Bool) async throws {
        _ = try await api.put("/api/me/active-status", body: ["show": show])
    }
}
