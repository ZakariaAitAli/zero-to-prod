GRANT SELECT (id, title, created_at)
ON TABLE public.work_items
TO zero_to_prod_app;

GRANT INSERT (title)
ON TABLE public.work_items
TO zero_to_prod_app;

GRANT USAGE
ON SEQUENCE public.work_items_id_seq
TO zero_to_prod_app;
