import SwiftUI

struct LoginView: View {
    @EnvironmentObject var session: SessionStore
    @State private var identifier = ""
    @State private var password = ""
    @State private var totp = ""
    @State private var needs2FA = false
    @State private var error: String?
    @State private var busy = false

    var body: some View {
        VStack(spacing: 16) {
            Text("ChatApp").font(.largeTitle).bold()
            TextField("Username / email / phone", text: $identifier)
                .textContentType(.username)
                .autocapitalization(.none)
                .textFieldStyle(.roundedBorder)
            SecureField("Password", text: $password)
                .textContentType(.password)
                .textFieldStyle(.roundedBorder)
            if needs2FA {
                TextField("2FA code", text: $totp)
                    .keyboardType(.numberPad)
                    .textFieldStyle(.roundedBorder)
            }
            if let error {
                Text(error).foregroundStyle(.red).font(.footnote)
            }
            Button(action: login) {
                Text(busy ? "…" : "Log in")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .disabled(busy)
        }
        .padding(24)
    }

    private func login() {
        busy = true
        error = nil
        Task {
            do {
                let data = try await APIClient().post("/api/auth/login", body: [
                    "identifier": identifier,
                    "password": password,
                    "totp_code": totp,
                ])
                let json = try JSONSerialization.jsonObject(with: data) as? [String: Any]
                session.accessToken = json?["access_token"] as? String
                session.refreshToken = json?["refresh_token"] as? String
                session.userId = json?["user_id"] as? String
            } catch let APIError.http(_, body) where body.contains("totp_required") {
                needs2FA = true
                error = "Enter your authenticator code"
            } catch {
                self.error = "Login failed"
            }
            busy = false
        }
    }
}
