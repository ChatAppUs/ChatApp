import SwiftUI
import AuthenticationServices

private func identifierType(_ value: String) -> String {
    let v = value.trimmingCharacters(in: .whitespacesAndNewlines)
    if v.isEmpty { return "unknown" }
    let allowed = CharacterSet(charactersIn: "+0123456789()-. \t")
    return v.rangeOfCharacter(from: CharacterSet(charactersIn: "@")) == nil && v.unicodeScalars.allSatisfy { allowed.contains($0) } ? "phone" : "email"
}

struct LoginView: View {
    @EnvironmentObject var session: SessionStore
    @State private var identifier = ""
    @State private var password = ""
    @State private var totp = ""
    @State private var needs2FA = false
    @State private var error: String?
    @State private var busy = false
    @State private var trustedLogin = false
    @State private var notFound = false
    @State private var rememberAndTrust = true

    var body: some View {
        VStack(spacing: 16) {
            Text("ChatApp").font(.largeTitle).bold()
            TextField("Username / email / phone", text: $identifier)
                .textContentType(.username)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .textFieldStyle(.roundedBorder)
                .onChange(of: identifier) { _ in notFound = false }
            if !trustedLogin {
                SecureField("Password", text: $password).textContentType(.password).textFieldStyle(.roundedBorder)
            }
            if needs2FA {
                TextField("2FA code", text: $totp).keyboardType(.numberPad).textFieldStyle(.roundedBorder)
            }
            if notFound {
                Text("No account found for “\(identifier)”. Create an account from the Sign up screen, then come back.")
                    .foregroundStyle(.orange).font(.footnote)
            }
            if let error { Text(error).foregroundStyle(.red).font(.footnote) }
            Button(action: login) {
                Text(busy ? "…" : (trustedLogin ? "Log in without password" : "Log in")).frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .disabled(busy || (trustedLogin && (session.deviceTrustToken ?? "").isEmpty))
            if (session.deviceTrustToken ?? "").isEmpty {
                Toggle("Remember me and trust this device for 30 days", isOn: $rememberAndTrust).font(.footnote)
            } else {
                Button(trustedLogin ? "Use password instead" : "This device is trusted — sign in without password") { trustedLogin.toggle(); error = nil }.font(.footnote)
            }
            SignInWithAppleButton(.signIn) { request in request.requestedScopes = [.fullName, .email] }
                onCompletion: { result in handleApple(result) }
                .signInWithAppleButtonStyle(.black)
                .frame(height: 44)
        }
        .padding(24)
    }

    private func handleApple(_ result: Result<ASAuthorization, Error>) {
        switch result {
        case .failure(let err): error = "Apple sign-in failed: \(err.localizedDescription)"
        case .success(let auth):
            guard let cred = auth.credential as? ASAuthorizationAppleIDCredential,
                  let tokenData = cred.identityToken,
                  let idToken = String(data: tokenData, encoding: .utf8) else {
                error = "Apple sign-in returned no identity token"; return
            }
            busy = true; error = nil
            Task {
                do {
                    let data = try await APIClient().post("/api/auth/apple", body: ["id_token": idToken, "totp_code": totp])
                    try apply(data)
                } catch let APIError.http(_, body) where body.contains("totp_required") {
                    needs2FA = true; error = "Enter your authenticator code"
                } catch { self.error = "Apple sign-in failed" }
                busy = false
            }
        }
    }

    private func login() {
        busy = true; error = nil
        Task {
            do {
                if trustedLogin {
                    let data = try await APIClient().post("/api/auth/trusted-device/login", body: ["device_token": session.deviceTrustToken ?? "", "totp_code": totp])
                    try apply(data)
                } else {
                    let trimmed = identifier.trimmingCharacters(in: .whitespacesAndNewlines)
                    let type = identifierType(trimmed)
                    let check = try await APIClient().post("/api/auth/identifier/check", body: ["identifier": trimmed])
                    if let json = try JSONSerialization.jsonObject(with: check) as? [String: Any], json["exists"] as? Bool != true {
                        await MainActor.run { notFound = true; busy = false }; return
                    }
                    let data = try await APIClient().post("/api/auth/login", body: [
                        "identifier": trimmed,
                        "type": type,
                        "password": password,
                        "totp_code": totp,
                    ])
                    try apply(data)
                    if rememberAndTrust { _ = try? await enrollTrustedDevice() }
                }
            } catch let APIError.http(_, body) where body.contains("totp_required") {
                needs2FA = true; error = "Enter your authenticator code"
            } catch { self.error = "Login failed" }
            busy = false
        }
    }

    private func enrollTrustedDevice() async throws -> Data? {
        guard let token = session.accessToken else { return nil }
        let data = try await APIClient().post("/api/auth/trusted-device/enroll", body: [:], headers: ["Authorization": "Bearer \(token)"])
        if let json = try JSONSerialization.jsonObject(with: data) as? [String: Any], let deviceToken = json["device_token"] as? String {
            await MainActor.run { session.deviceTrustToken = deviceToken }
        }
        return data
    }

    private func apply(_ data: Data) throws {
        guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] else { throw APIError.http(0, "malformed response") }
        session.accessToken = json["access_token"] as? String
        session.refreshToken = json["refresh_token"] as? String
        session.userId = json["user_id"] as? String
        if let deviceToken = json["device_token"] as? String { session.deviceTrustToken = deviceToken }
    }
}
