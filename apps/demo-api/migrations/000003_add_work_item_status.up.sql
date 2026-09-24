ALTER TABLE public.work_items
ADD COLUMN status TEXT NOT NULL DEFAULT 'pending';

ALTER TABLE public.work_items
ADD CONSTRAINT work_items_status_check
CHECK (status IN ('pending', 'done'));

GRANT SELECT (status)
ON TABLE public.work_items
TO zero_to_prod_app;

GRANT INSERT (status)
ON TABLE public.work_items
TO zero_to_prod_app;
