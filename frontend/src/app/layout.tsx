// TODO: root layout with Tailwind + shadcn/ui provider, sidebar nav
import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "RecoverAI — Revenue Recovery Dashboard",
  description: "Autonomous payment recovery for Razorpay merchants",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
