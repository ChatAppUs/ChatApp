public let kSecRandomDefault = SecRandomRef()
public struct SecRandomRef { public init() {} }
@discardableResult
public func SecRandomCopyBytes(_ rnd: SecRandomRef, _ count: Int, _ bytes: UnsafeMutableRawPointer) -> Int32 { 0 }
