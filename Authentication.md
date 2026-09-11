PROJECT REQUIREMENT:
Light/Dark theme switch must work everywhere on every page across all platforms.
All systems must be fully dynamic, production-ready, scalable, secure, and implemented with complete real business logic. No simulation, mock data, placeholder workflows, or partial implementations are allowed.
The system must be reviewed end-to-end to identify all missing, incomplete, duplicated, disconnected, or broken components across frontend, backend, database, blockchain, and infrastructure layers.
==================================================
GLOBAL PLATFORM ARCHITECTURE
==================================================
The ecosystem must include fully functional Admin and User applications across:
• Android (Java + Kotlin)
• iOS (Swift)
• Windows Desktop 
• Linux Desktop 
• macOS Desktop 
• Web Application
• 

All platforms must ensure:
• Unified Light/Dark theme across every screen
• Fully responsive UI/UX (mobile, desktop, web)
• Real-time synchronization across all systems
• Full frontend ↔ backend integration (no isolated modules)
• Enterprise-grade scalability and high availability
• Production-level security and encryption
• High-performance architecture with optimized latency
• Fully dynamic operational logic only
All authentication and identity-related forms must use ONE unified input field only (no toggle, no switch UI).
This applies to:
Login, Register, Reset Password, 2FA Reset, Email/Phone Change, Account Deletion, Recovery flows.
CORE BEHAVIOR
Single Input Box:
• Only one visible input field
• No manual email/phone switch UI
• No dropdown mode selector
AUTO DETECTION LOGIC (REAL-TIME)
PHONE MODE (Numeric Input Detected):
• Auto switch to Phone Mode
• Show country flag selector
• Show international dial code
• Support 200+ countries
• Searchable dropdown
• Auto-format number based on region
• Validate phone structure in real time
EMAIL MODE (Alphabet / Email Pattern Detected):
• Auto switch to Email Mode
• Hide country selector
• Enable email validation engine
• Apply RFC-style email validation
BEHAVIOR RULES
• Instant switching without refresh
• No UI flicker or reload
• Supports paste/autofill and autocomplete detection
• Backspace dynamically recalculates mode
• Seamless transition without losing input
• Backend receives explicit type flag (email/phone)
3.1 LOGIN 
• Unified smart input (email/phone auto detection)
• Account existence check in real time
• Auto redirect to SignUp if not registered
• Continue button work 
• Password field with visibility toggle (👁️)
• 5 failed attempts → 48-hour account lock
• Remember Me (localStorage persistent session)
• Submit button work 
Flow:
• Email OTP (6-digit)
• Phone OTP (6-digit)
• Conditional 2FA (if enabled)
• Skip 2FA if disabled
• Trusted device login (30-day passwordless option)
• Login button work 
• Redirect to User Home
Features:
• Forgot Password
• Social login:
Google OAuth 
Apple OAuth 
• Social login required mail verification and phone and 2fa verification if enabled  
• Login button work
• redirect home 
• Passkey authentication
• 
• Loading spinner
• Error/success messages
• Back to home link
3.2 REGISTER 
• Smart email/phone detection unified input
• Duplicate account prevention check
• Continue button work 
• next Otp verification 
• Email/phone OTP verification required
• Continue button work 
• next password field 
Password:
• Minimum 8 characters
• Strength indicator:
🔴 Weak
🟡 Medium
🟢 Strong
• Confirm password validation
• Referral code optional
•  Terms & Conditions required
• SignUp button work  
•if done redirect home
Auth options:
• Google/Apple/ OAuth etc
•Once done Redirect home
• Passkey authentication
• MetaMask wallet login (DEX only)
UI:
• Loading spinner
• Success/error messages
• Back home
3.3 RESET PASSWORD
•emai/phone smart input detection unified
• Account exits check
•if not exist redirect Signup
•othewise Continue button work
•next verifications
• Email OTP (6-digit)
• Phone OTP (6-digit)
• 2FA verification (if enabled)
• 2FA recovery flow if lost
• Redirect to Home after success
3.4 2FA RESET SYSTEM
• email/phone smart detection unified 
• exist check if not redirect Signup
• exist the continue button work
• OTP verifications
• Email OTP verification
• Phone OTP verification
• continue button work 
• Iive verification
• KYC face match validation
• Live liveness detection (random instructions)
• Auto verification within 5 seconds
• Remove old 2FA automatically
• Allow new 2FA setup
• Redirect to Home
Mandatory before financial access:
KYC Verification:-
• Email verification
• Phone verification
User data:
• First name
• Last name
• Title
• Address
• City
• State/Division
• Postal code
• Country
Documents: (nid/passport/driving licence)
• Front ID
• Back ID
• Selfie with document
Live verification:
• Liveness engine
• Instruction-based verification
• Progress meter
• Auto-submit
• Admin review system
• Status tracking for users
Rule:
• No withdrawals without approved KYC
Security:
Any change in Email, Phone, Password, or 2FA triggers:
• 48-hour withdrawal freeze
• Auto re-enable after cooldown
• Smart detection input
• Verify current email/phone
• Verify new email/phone
• OTP verification
• KYC-linked face verification
• 5-second liveness confirmation
• Global update propagation
Result:
• Old credentials removed
• New credentials activated system-wide
Verification required:
• Email OTP
• Phone OTP
• Liveness verification
• “I have withdrawn all assets” checkbox
Flow:
• 30-day pending deletion
• Login during the grace period cancels deletion
• Permanent deletion after 30 days
• No recovery after final deletion
• Auto logout after request

All phone inputs must support:
• Country flags
• Country codes
• 200+ countries
