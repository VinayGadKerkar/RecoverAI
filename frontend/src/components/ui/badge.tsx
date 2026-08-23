import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-md border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
  {
    variants: {
      variant: {
        default: "border-transparent bg-primary text-primary-foreground",
        secondary: "border-transparent bg-secondary text-secondary-foreground",
        destructive: "border-transparent bg-destructive text-destructive-foreground",
        outline: "text-foreground",
        // Status-specific variants
        open: "border-yellow-900/50 bg-yellow-950 text-yellow-400",
        in_progress: "border-blue-900/50 bg-blue-950 text-blue-400",
        recovered: "border-green-900/50 bg-green-950 text-green-400",
        partially_recovered: "border-teal-900/50 bg-teal-950 text-teal-400",
        failed: "border-red-900/50 bg-red-950 text-red-400",
        pending_human_approval: "border-orange-900/50 bg-orange-950 text-orange-400",
        customer_self_recovered: "border-slate-700/50 bg-slate-800 text-slate-400",
        outage_batched: "border-purple-900/50 bg-purple-950 text-purple-400",
        not_worth_recovering: "border-slate-700/50 bg-slate-800 text-slate-500",
        stopped: "border-gray-700/50 bg-gray-800 text-gray-400",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  );
}

export { Badge, badgeVariants };
