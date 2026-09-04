import type { Metadata } from "next";
import Link from "next/link";
import { Toaster } from 'sonner';
import "./globals.css";

export const metadata: Metadata = {
  title: "RecoverAI — Revenue Recovery Dashboard",
  description: "Autonomous payment recovery for Razorpay merchants",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark">
      <body className="min-h-screen bg-background font-sans antialiased">
        <div className="flex min-h-screen">
          {/* Sidebar */}
          <aside className="w-64 border-r border-border bg-card p-6">
            <div className="mb-8">
              <h1 className="text-2xl font-bold text-foreground">RecoverAI</h1>
              <p className="text-sm text-muted-foreground">Revenue Recovery</p>
            </div>
            <nav className="space-y-2">
              <Link
                href="/dashboard"
                className="block rounded-lg px-4 py-2 text-sm font-medium text-foreground hover:bg-accent"
              >
                Overview
              </Link>
              <Link
                href="/dashboard/cases"
                className="block rounded-lg px-4 py-2 text-sm font-medium text-foreground hover:bg-accent"
              >
                Recovery Cases
              </Link>
              <Link
                href="/dashboard/analytics"
                className="block rounded-lg px-4 py-2 text-sm font-medium text-foreground hover:bg-accent"
              >
                Analytics
              </Link>
            </nav>
          </aside>

          {/* Main content */}
          <main className="flex-1 overflow-auto">{children}</main>
        </div>

        {/* Toast notifications */}
        <Toaster position="bottom-right" theme="dark" richColors />
      </body>
    </html>
  );
}
