import Link from "next/link";

export default function HomePage() {
  return (
    <div className="flex h-screen items-center justify-center bg-[#050508] flex-col gap-4">
      <h1 className="text-white text-2xl">RecoverAI</h1>
      <Link href="/login" className="text-emerald-500 hover:underline">Go to Login</Link>
      <Link href="/dashboard" className="text-emerald-500 hover:underline">Go to Dashboard</Link>
    </div>
  );
}
