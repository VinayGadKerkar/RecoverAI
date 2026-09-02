import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-3 py-1 text-xs font-semibold transition-all duration-200",
  {
    variants: {
      variant: {
        default: "border-primary/30 bg-primary/10 text-primary",
        secondary: "border-secondary/30 bg-secondary/10 text-secondary-foreground",
        destructive: "border-destructive/30 bg-destructive/10 text-destructive",
        outline: "border-border text-foreground",
        
        // Enhanced status-specific variants with semantic colors
        open: "border-yellow-500/30 bg-yellow-500/10 text-yellow-400 hover:bg-yellow-500/20",
        in_progress: "border-blue-500/30 bg-blue-500/10 text-blue-400 hover:bg-blue-500/20",
        recovered: "border-green-500/30 bg-green-500/10 text-green-400 hover:bg-green-500/20",
        partially_recovered: "border-teal-500/30 bg-teal-500/10 text-teal-400 hover:bg-teal-500/20",
        failed: "border-red-500/30 bg-red-500/10 text-red-400 hover:bg-red-500/20",
        pending_human_approval: "border-orange-500/30 bg-orange-500/10 text-orange-400 hover:bg-orange-500/20",
        customer_self_recovered: "border-slate-500/30 bg-slate-500/10 text-slate-300 hover:bg-slate-500/20",
        outage_batched: "border-purple-500/30 bg-purple-500/10 text-purple-400 hover:bg-purple-500/20",
        not_worth_recovering: "border-slate-500/30 bg-slate-500/10 text-slate-400 hover:bg-slate-500/20",
        stopped: "border-gray-500/30 bg-gray-500/10 text-gray-400 hover:bg-gray-500/20",
        
        // Additional utility variants
        success: "border-green-500/30 bg-green-500/10 text-green-400 hover:bg-green-500/20",
        warning: "border-orange-500/30 bg-orange-500/10 text-orange-400 hover:bg-orange-500/20",
        info: "border-blue-500/30 bg-blue-500/10 text-blue-400 hover:bg-blue-500/20",
        danger: "border-red-500/30 bg-red-500/10 text-red-400 hover:bg-red-500/20",
        muted: "border-slate-500/30 bg-slate-500/10 text-slate-400 hover:bg-slate-500/20",
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
